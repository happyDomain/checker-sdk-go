// Copyright 2020-2026 The happyDomain Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxRequestBodySize is the maximum allowed size for incoming request bodies (1 MB).
const maxRequestBodySize = 1 << 20

// loadSampleInterval is how often the background sampler updates the
// exponentially weighted moving averages reported in HealthResponse.LoadAvg.
// 5 seconds matches the Unix kernel's loadavg cadence.
const loadSampleInterval = 5 * time.Second

// EWMA smoothing factors for 1, 5, and 15-minute windows sampled every
// loadSampleInterval. Derived as 1 - exp(-interval/window) so that the
// steady-state response to a constant InFlight of N converges to N.
var (
	loadAlpha1  = 1 - math.Exp(-float64(loadSampleInterval)/float64(1*time.Minute))
	loadAlpha5  = 1 - math.Exp(-float64(loadSampleInterval)/float64(5*time.Minute))
	loadAlpha15 = 1 - math.Exp(-float64(loadSampleInterval)/float64(15*time.Minute))
)

// updateLoadAvg advances the three EWMAs by one tick given the current
// InFlight sample. It is a pure function to keep the sampler trivially testable.
func updateLoadAvg(prev [3]float64, sample float64) [3]float64 {
	return [3]float64{
		prev[0] + loadAlpha1*(sample-prev[0]),
		prev[1] + loadAlpha5*(sample-prev[1]),
		prev[2] + loadAlpha15*(sample-prev[2]),
	}
}

// Server is a generic HTTP server for external checkers.
// It always exposes /health and /collect. If the provider implements
// CheckerDefinitionProvider, it also exposes /definition and /evaluate.
// If the provider implements CheckerHTMLReporter or CheckerMetricsReporter,
// it also exposes /report.
//
// Security: Server does not perform any authentication or authorization.
// It is intended to be run behind a reverse proxy or in a trusted network
// where access control is handled externally (e.g. by the happyDomain server).
type Server struct {
	provider   ObservationProvider
	definition *CheckerDefinition
	mux        *http.ServeMux

	// startTime is captured in NewServer and used to compute uptime.
	startTime time.Time

	// inFlight counts work requests (/collect, /evaluate, /report) currently
	// being processed. /health and /definition are not tracked.
	inFlight atomic.Int64

	// totalRequests is the cumulative number of work requests served.
	totalRequests atomic.Uint64

	// loadBits stores the 1, 5, 15-minute EWMAs of inFlight as float64 bit
	// patterns (math.Float64bits) so reads and writes are tear-free and
	// lock-free across the sampler goroutine and the /health handler.
	loadBits [3]atomic.Uint64

	// cancelSampler stops the background load-average sampler.
	cancelSampler context.CancelFunc

	// samplerDone is closed when the sampler goroutine returns.
	samplerDone chan struct{}

	// closeOnce guarantees Close is idempotent.
	closeOnce sync.Once
}

// NewServer creates a new checker HTTP server backed by the given provider.
// Additional endpoints are registered based on optional interfaces the provider implements.
//
// NewServer also starts a background goroutine that samples the in-flight
// request count every loadSampleInterval to compute the load averages
// reported on /health. Call Close to stop it.
func NewServer(provider ObservationProvider) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		provider:      provider,
		startTime:     time.Now(),
		cancelSampler: cancel,
		samplerDone:   make(chan struct{}),
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.Handle("POST /collect", s.trackWork(http.HandlerFunc(s.handleCollect)))

	if dp, ok := provider.(CheckerDefinitionProvider); ok {
		s.definition = dp.Definition()
		s.definition.BuildRulesInfo()
		s.mux.HandleFunc("GET /definition", s.handleDefinition)
		s.mux.Handle("POST /evaluate", s.trackWork(http.HandlerFunc(s.handleEvaluate)))
	}

	if _, ok := provider.(CheckerHTMLReporter); ok {
		s.mux.Handle("POST /report", s.trackWork(http.HandlerFunc(s.handleReport)))
	} else if _, ok := provider.(CheckerMetricsReporter); ok {
		s.mux.Handle("POST /report", s.trackWork(http.HandlerFunc(s.handleReport)))
	}

	go s.runSampler(ctx)

	return s
}

// Handler returns the http.Handler for this server, allowing callers
// to embed it in a custom server or add middleware.
func (s *Server) Handler() http.Handler {
	return requestLogger(s.mux)
}

// ListenAndServe starts the HTTP server on the given address.
//
// ListenAndServe does not stop the background load-average sampler on return;
// call Close to stop it. This is not required for process-scoped usage but is
// recommended for tests and embedded lifecycles.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("checker listening on %s", addr)
	return http.ListenAndServe(addr, requestLogger(s.mux))
}

// Close stops the background load-average sampler goroutine. It is safe to
// call multiple times; subsequent calls are no-ops. Close does not shut down
// any underlying http.Server — callers own that lifecycle.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.cancelSampler()
		<-s.samplerDone
	})
	return nil
}

// trackWork wraps a handler with in-flight and total-request accounting.
// It is applied only to "work" endpoints (/collect, /evaluate, /report) so
// that /health polling traffic does not pollute the load signal.
func (s *Server) trackWork(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inFlight.Add(1)
		s.totalRequests.Add(1)
		defer s.inFlight.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// runSampler updates the load-average EWMAs every loadSampleInterval until
// ctx is canceled. It closes s.samplerDone on exit.
func (s *Server) runSampler(ctx context.Context) {
	defer close(s.samplerDone)
	ticker := time.NewTicker(loadSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var prev [3]float64
			for i := range prev {
				prev[i] = math.Float64frombits(s.loadBits[i].Load())
			}
			next := updateLoadAvg(prev, float64(s.inFlight.Load()))
			for i := range next {
				s.loadBits[i].Store(math.Float64bits(next[i]))
			}
		}
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var load [3]float64
	for i := range load {
		load[i] = math.Float64frombits(s.loadBits[i].Load())
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:        "ok",
		Uptime:        time.Since(s.startTime).Seconds(),
		NumCPU:        runtime.NumCPU(),
		InFlight:      s.inFlight.Load(),
		TotalRequests: s.totalRequests.Load(),
		LoadAvg:       load,
	})
}

func (s *Server) handleDefinition(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.definition)
}

func (s *Server) handleCollect(w http.ResponseWriter, r *http.Request) {
	var req ExternalCollectRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodySize)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ExternalCollectResponse{
			Error: fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	data, err := s.provider.Collect(r.Context(), req.Options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ExternalCollectResponse{
			Error: err.Error(),
		})
		return
	}

	raw, err := json.Marshal(data)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ExternalCollectResponse{
			Error: fmt.Sprintf("failed to marshal result: %v", err),
		})
		return
	}

	resp := ExternalCollectResponse{Data: json.RawMessage(raw)}

	// Harvest discovery entries from the native Go value, before it goes
	// out of scope. No re-parse; DiscoverEntries operates on the same
	// object that was just marshaled above.
	if dp, ok := s.provider.(DiscoveryPublisher); ok {
		entries, derr := dp.DiscoverEntries(data)
		if derr != nil {
			log.Printf("DiscoverEntries failed: %v", derr)
		} else {
			resp.Entries = entries
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req ExternalEvaluateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodySize)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ExternalEvaluateResponse{
			Error: fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	obs := &mapObservationGetter{data: req.Observations}

	var states []CheckState
	for _, rule := range s.definition.Rules {
		if len(req.EnabledRules) > 0 {
			if enabled, ok := req.EnabledRules[rule.Name()]; ok && !enabled {
				continue
			}
		}
		state := rule.Evaluate(r.Context(), obs, req.Options)
		if state.Code == "" {
			state.Code = rule.Name()
		}
		states = append(states, state)
	}

	writeJSON(w, http.StatusOK, ExternalEvaluateResponse{States: states})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var req ExternalReportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodySize)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "text/html") {
		reporter, ok := s.provider.(CheckerHTMLReporter)
		if !ok {
			http.Error(w, "this checker does not support HTML reports", http.StatusNotImplemented)
			return
		}

		html, err := reporter.GetHTMLReport(req.Data)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to generate HTML report: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	// Default: JSON metrics.
	reporter, ok := s.provider.(CheckerMetricsReporter)
	if !ok {
		http.Error(w, "this checker does not support metrics reports", http.StatusNotImplemented)
		return
	}

	metrics, err := reporter.ExtractMetrics(req.Data, time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to extract metrics: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

// mapObservationGetter implements ObservationGetter backed by a static map.
type mapObservationGetter struct {
	data map[ObservationKey]json.RawMessage
}

func (g *mapObservationGetter) Get(ctx context.Context, key ObservationKey, dest any) error {
	raw, ok := g.data[key]
	if !ok {
		return fmt.Errorf("observation %q not available", key)
	}
	return json.Unmarshal(raw, dest)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
