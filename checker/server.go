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
	"net/http"
	"strings"
	"time"
)

// maxRequestBodySize is the maximum allowed size for incoming request bodies (1 MB).
const maxRequestBodySize = 1 << 20

// Server is a generic HTTP server for external checkers.
// It always exposes /health and /collect. If the provider implements
// CheckerDefinitionProvider, it also exposes /definition and /evaluate.
// If the provider implements CheckerHTMLReporter or CheckerMetricsReporter,
// it also exposes /report.
type Server struct {
	provider   ObservationProvider
	definition *CheckerDefinition
	mux        *http.ServeMux
}

// NewServer creates a new checker HTTP server backed by the given provider.
// Additional endpoints are registered based on optional interfaces the provider implements.
func NewServer(provider ObservationProvider) *Server {
	s := &Server{provider: provider}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /collect", s.handleCollect)

	if dp, ok := provider.(CheckerDefinitionProvider); ok {
		s.definition = dp.Definition()
		s.definition.BuildRulesInfo()
		s.mux.HandleFunc("GET /definition", s.handleDefinition)
		s.mux.HandleFunc("POST /evaluate", s.handleEvaluate)
	}

	if _, ok := provider.(CheckerHTMLReporter); ok {
		s.mux.HandleFunc("POST /report", s.handleReport)
	} else if _, ok := provider.(CheckerMetricsReporter); ok {
		s.mux.HandleFunc("POST /report", s.handleReport)
	}

	return s
}

// Handler returns the http.Handler for this server, allowing callers
// to embed it in a custom server or add middleware.
func (s *Server) Handler() http.Handler {
	return requestLogger(s.mux)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("checker listening on %s", addr)
	return http.ListenAndServe(addr, requestLogger(s.mux))
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		writeJSON(w, http.StatusOK, ExternalCollectResponse{
			Error: err.Error(),
		})
		return
	}

	raw, err := json.Marshal(data)
	if err != nil {
		writeJSON(w, http.StatusOK, ExternalCollectResponse{
			Error: fmt.Sprintf("failed to marshal result: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, ExternalCollectResponse{
		Data: json.RawMessage(raw),
	})
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
