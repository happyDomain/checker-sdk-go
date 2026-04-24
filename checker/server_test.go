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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// --- test doubles ---

type testProvider struct {
	key        ObservationKey
	collectFn  func(ctx context.Context, opts CheckerOptions) (any, error)
	definition *CheckerDefinition
	htmlFn     func(raw json.RawMessage) (string, error)
	metricsFn  func(raw json.RawMessage, t time.Time) ([]CheckMetric, error)
}

func (p *testProvider) Key() ObservationKey { return p.key }
func (p *testProvider) Collect(ctx context.Context, opts CheckerOptions) (any, error) {
	if p.collectFn != nil {
		return p.collectFn(ctx, opts)
	}
	return map[string]string{"result": "ok"}, nil
}
func (p *testProvider) Definition() *CheckerDefinition { return p.definition }
func (p *testProvider) GetHTMLReport(ctx ReportContext) (string, error) {
	if p.htmlFn != nil {
		return p.htmlFn(ctx.Data())
	}
	return "<h1>report</h1>", nil
}
func (p *testProvider) ExtractMetrics(ctx ReportContext, t time.Time) ([]CheckMetric, error) {
	if p.metricsFn != nil {
		return p.metricsFn(ctx.Data(), t)
	}
	return []CheckMetric{{Name: "m1", Value: 1.0, Timestamp: t}}, nil
}

// dummyRule is a minimal CheckRule for testing evaluate.
type dummyRule struct {
	name string
	desc string
}

func (r *dummyRule) Name() string        { return r.name }
func (r *dummyRule) Description() string { return r.desc }
func (r *dummyRule) Evaluate(ctx context.Context, obs ObservationGetter, opts CheckerOptions) []CheckState {
	return []CheckState{{Status: StatusOK, Message: r.name + " passed"}}
}

// codedRule emits a CheckState with a pre-set Code, to verify the server
// stamps RuleName without clobbering rule-provided codes.
type codedRule struct {
	name, code string
}

func (r *codedRule) Name() string        { return r.name }
func (r *codedRule) Description() string { return "" }
func (r *codedRule) Evaluate(ctx context.Context, obs ObservationGetter, opts CheckerOptions) []CheckState {
	return []CheckState{{Status: StatusWarn, Code: r.code, Message: "coded finding"}}
}

// --- helpers ---

func newTestServer(p *testProvider) *Server {
	return NewServer(p)
}

func doRequest(handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestServer_Health(t *testing.T) {
	p := &testProvider{key: "test", definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}}}
	srv := newTestServer(p)
	defer srv.Close()
	rec := doRequest(srv.Handler(), "GET", "/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("GET /health status = %q, want \"ok\"", resp.Status)
	}
	if resp.NumCPU <= 0 {
		t.Errorf("NumCPU = %d, want > 0", resp.NumCPU)
	}
	if resp.Uptime < 0 {
		t.Errorf("Uptime = %v, want >= 0", resp.Uptime)
	}
	if resp.InFlight != 0 {
		t.Errorf("InFlight = %d on fresh server, want 0", resp.InFlight)
	}
	if resp.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d on fresh server, want 0", resp.TotalRequests)
	}
	if resp.LoadAvg != [3]float64{0, 0, 0} {
		t.Errorf("LoadAvg = %v on fresh server, want all zero", resp.LoadAvg)
	}
}

func TestServer_Health_TracksInFlight(t *testing.T) {
	release := make(chan struct{})
	var collectEntered sync.WaitGroup
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}},
		collectFn: func(ctx context.Context, opts CheckerOptions) (any, error) {
			collectEntered.Done()
			<-release
			return map[string]string{"ok": "1"}, nil
		},
	}
	srv := newTestServer(p)
	defer srv.Close()
	handler := srv.Handler()

	const n = 3
	collectEntered.Add(n)
	var clientsDone sync.WaitGroup
	clientsDone.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer clientsDone.Done()
			doRequest(handler, "POST", "/collect", ExternalCollectRequest{Key: "test"}, nil)
		}()
	}

	// Wait for all n handlers to be inside collectFn (== all n in-flight).
	collectEntered.Wait()

	// Record /health mid-flight. Also hammer it to verify /health polls
	// do not inflate InFlight or TotalRequests.
	var mid HealthResponse
	for i := 0; i < 5; i++ {
		rec := doRequest(handler, "GET", "/health", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /health = %d, want %d", rec.Code, http.StatusOK)
		}
		if err := json.NewDecoder(rec.Body).Decode(&mid); err != nil {
			t.Fatalf("decode /health: %v", err)
		}
	}
	if mid.InFlight != n {
		t.Errorf("mid-flight InFlight = %d, want %d", mid.InFlight, n)
	}
	if mid.TotalRequests != n {
		t.Errorf("mid-flight TotalRequests = %d, want %d (health polls must not count)", mid.TotalRequests, n)
	}

	// Release all work and wait for clients to return.
	close(release)
	clientsDone.Wait()

	rec := doRequest(handler, "GET", "/health", nil, nil)
	var after HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&after); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if after.InFlight != 0 {
		t.Errorf("post-flight InFlight = %d, want 0", after.InFlight)
	}
	if after.TotalRequests != n {
		t.Errorf("post-flight TotalRequests = %d, want %d", after.TotalRequests, n)
	}
	if after.Uptime < mid.Uptime {
		t.Errorf("Uptime went backwards: mid=%v after=%v", mid.Uptime, after.Uptime)
	}
}

func TestUpdateLoadAvg(t *testing.T) {
	load := [3]float64{0, 0, 0}
	for i := 0; i < 20; i++ {
		load = updateLoadAvg(load, 5)
	}
	if !(load[0] > load[1] && load[1] > load[2]) {
		t.Errorf("expected load[0] > load[1] > load[2], got %v", load)
	}
	for i, v := range load {
		if v <= 0 {
			t.Errorf("load[%d] = %v, want > 0", i, v)
		}
		if v >= 5 {
			t.Errorf("load[%d] = %v, want < 5 (not yet converged)", i, v)
		}
	}

	// Constant sample of zero from a non-zero state must decay toward zero.
	decaying := load
	for i := 0; i < 50; i++ {
		decaying = updateLoadAvg(decaying, 0)
	}
	for i := range decaying {
		if decaying[i] >= load[i] {
			t.Errorf("decaying[%d] = %v, want < %v", i, decaying[i], load[i])
		}
	}
}

func TestServer_Close_Idempotent(t *testing.T) {
	p := &testProvider{key: "test", definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}}}
	srv := newTestServer(p)
	done := make(chan error, 2)
	go func() { done <- srv.Close() }()
	go func() { done <- srv.Close() }()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Close() returned %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Close() deadlocked")
		}
	}
}

func TestServer_Collect_Success(t *testing.T) {
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}},
		collectFn: func(ctx context.Context, opts CheckerOptions) (any, error) {
			return map[string]int{"count": 42}, nil
		},
	}
	srv := newTestServer(p)
	rec := doRequest(srv.Handler(), "POST", "/collect", ExternalCollectRequest{
		Key:     "test",
		Options: CheckerOptions{"a": "b"},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /collect = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp ExternalCollectResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "" {
		t.Errorf("POST /collect error = %q, want empty", resp.Error)
	}
	if resp.Data == nil {
		t.Fatal("POST /collect data is nil")
	}
}

func TestServer_Collect_ProviderError(t *testing.T) {
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}},
		collectFn: func(ctx context.Context, opts CheckerOptions) (any, error) {
			return nil, errors.New("provider failed")
		},
	}
	srv := newTestServer(p)
	rec := doRequest(srv.Handler(), "POST", "/collect", ExternalCollectRequest{Key: "test"}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /collect = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var resp ExternalCollectResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error == "" {
		t.Error("expected error in response, got empty")
	}
}

func TestServer_Collect_BadBody(t *testing.T) {
	p := &testProvider{key: "test", definition: &CheckerDefinition{ID: "test", Rules: []CheckRule{}}}
	srv := newTestServer(p)
	req := httptest.NewRequest("POST", "/collect", bytes.NewBufferString("{invalid"))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /collect bad body = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServer_Definition(t *testing.T) {
	def := &CheckerDefinition{
		ID:   "test-checker",
		Name: "Test Checker",
		Rules: []CheckRule{
			&dummyRule{name: "rule1", desc: "first rule"},
		},
	}
	p := &testProvider{key: "test", definition: def}
	srv := newTestServer(p)
	rec := doRequest(srv.Handler(), "GET", "/definition", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /definition = %d, want %d", rec.Code, http.StatusOK)
	}
	var got CheckerDefinition
	json.NewDecoder(rec.Body).Decode(&got)
	if got.ID != "test-checker" {
		t.Errorf("definition ID = %q, want \"test-checker\"", got.ID)
	}
	if len(got.RulesInfo) != 1 {
		t.Errorf("definition rules = %d, want 1", len(got.RulesInfo))
	}
}

func TestServer_Evaluate(t *testing.T) {
	def := &CheckerDefinition{
		ID:   "test-checker",
		Name: "Test Checker",
		Rules: []CheckRule{
			&dummyRule{name: "rule1", desc: "first rule"},
			&dummyRule{name: "rule2", desc: "second rule"},
		},
	}
	p := &testProvider{key: "test", definition: def}
	srv := newTestServer(p)

	rec := doRequest(srv.Handler(), "POST", "/evaluate", ExternalEvaluateRequest{
		Observations: map[ObservationKey]json.RawMessage{
			"test": json.RawMessage(`{"count":42}`),
		},
		Options: CheckerOptions{},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /evaluate = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp ExternalEvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.States) != 2 {
		t.Fatalf("evaluate states = %d, want 2", len(resp.States))
	}
	if resp.States[0].RuleName != "rule1" {
		t.Errorf("evaluate state[0].RuleName = %q, want \"rule1\"", resp.States[0].RuleName)
	}
	if resp.States[0].Code != "" {
		t.Errorf("evaluate state[0].Code = %q, want empty (rule did not set one)", resp.States[0].Code)
	}
}

func TestServer_Evaluate_DisabledRule(t *testing.T) {
	def := &CheckerDefinition{
		ID: "test-checker",
		Rules: []CheckRule{
			&dummyRule{name: "rule1", desc: "first"},
			&dummyRule{name: "rule2", desc: "second"},
		},
	}
	p := &testProvider{key: "test", definition: def}
	srv := newTestServer(p)

	rec := doRequest(srv.Handler(), "POST", "/evaluate", ExternalEvaluateRequest{
		Observations: map[ObservationKey]json.RawMessage{
			"test": json.RawMessage(`{}`),
		},
		EnabledRules: map[string]bool{"rule1": false},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /evaluate = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp ExternalEvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.States) != 1 {
		t.Fatalf("evaluate with disabled rule: states = %d, want 1", len(resp.States))
	}
	if resp.States[0].RuleName != "rule2" {
		t.Errorf("remaining state rule name = %q, want \"rule2\"", resp.States[0].RuleName)
	}
}

func TestServer_Evaluate_RulePreservesCode(t *testing.T) {
	def := &CheckerDefinition{
		ID: "test-checker",
		Rules: []CheckRule{
			&codedRule{name: "ruleA", code: "too_many_lookups"},
		},
	}
	p := &testProvider{key: "test", definition: def}
	srv := newTestServer(p)

	rec := doRequest(srv.Handler(), "POST", "/evaluate", ExternalEvaluateRequest{
		Observations: map[ObservationKey]json.RawMessage{"test": json.RawMessage(`{}`)},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /evaluate = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp ExternalEvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.States) != 1 {
		t.Fatalf("states = %d, want 1", len(resp.States))
	}
	if resp.States[0].RuleName != "ruleA" {
		t.Errorf("state.RuleName = %q, want \"ruleA\"", resp.States[0].RuleName)
	}
	if resp.States[0].Code != "too_many_lookups" {
		t.Errorf("state.Code = %q, want \"too_many_lookups\" (rule-set code must be preserved)", resp.States[0].Code)
	}
}

func TestServer_Report_HTML(t *testing.T) {
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test-checker", Rules: []CheckRule{}},
		htmlFn: func(raw json.RawMessage) (string, error) {
			return "<p>hello</p>", nil
		},
	}
	srv := newTestServer(p)
	rec := doRequest(srv.Handler(), "POST", "/report", ExternalReportRequest{
		Key:  "test",
		Data: json.RawMessage(`{}`),
	}, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /report html = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if body := rec.Body.String(); body != "<p>hello</p>" {
		t.Errorf("body = %q, want \"<p>hello</p>\"", body)
	}
}

func TestServer_Report_Metrics(t *testing.T) {
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test-checker", Rules: []CheckRule{}},
	}
	srv := newTestServer(p)
	rec := doRequest(srv.Handler(), "POST", "/report", ExternalReportRequest{
		Key:  "test",
		Data: json.RawMessage(`{}`),
	}, map[string]string{"Accept": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /report metrics = %d, want %d", rec.Code, http.StatusOK)
	}
	var metrics []CheckMetric
	json.NewDecoder(rec.Body).Decode(&metrics)
	if len(metrics) != 1 {
		t.Errorf("metrics count = %d, want 1", len(metrics))
	}
}

// TestServer_Report_Related verifies the remote /report path wires
// ExternalReportRequest.Related through to the provider's ReportContext,
// the fix for the "remote checkers can't see related observations" gap.
func TestServer_Report_Related(t *testing.T) {
	var gotRelated []RelatedObservation
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test-checker", Rules: []CheckRule{}},
	}
	// Replace htmlFn with one that peeks at a related key. We can't do that
	// directly through testProvider's htmlFn (which only sees raw), so
	// bind to GetHTMLReport via an inline wrapper: use a per-test provider
	// that captures the ReportContext before delegating to the template.
	srv := NewServer(&relatedPeekingProvider{
		base:   p,
		target: &gotRelated,
	})
	defer srv.Close()

	req := ExternalReportRequest{
		Key:  "test",
		Data: json.RawMessage(`{}`),
		Related: map[ObservationKey][]RelatedObservation{
			"tls_probes": {
				{CheckerID: "tls", Key: "tls_probes", Data: json.RawMessage(`{"ok":true}`), Ref: "ep-1"},
			},
		},
	}
	rec := doRequest(srv.Handler(), "POST", "/report", req, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /report = %d, want 200", rec.Code)
	}
	if len(gotRelated) != 1 {
		t.Fatalf("provider saw %d related observations, want 1", len(gotRelated))
	}
	if gotRelated[0].CheckerID != "tls" || string(gotRelated[0].Data) != `{"ok":true}` {
		t.Errorf("related mismatch: got %+v", gotRelated[0])
	}
}

// relatedPeekingProvider forwards to a base testProvider but copies the
// Related("tls_probes") slice observed at GetHTMLReport time into target.
type relatedPeekingProvider struct {
	base   *testProvider
	target *[]RelatedObservation
}

func (p *relatedPeekingProvider) Key() ObservationKey { return p.base.Key() }
func (p *relatedPeekingProvider) Collect(ctx context.Context, opts CheckerOptions) (any, error) {
	return p.base.Collect(ctx, opts)
}
func (p *relatedPeekingProvider) Definition() *CheckerDefinition { return p.base.definition }
func (p *relatedPeekingProvider) GetHTMLReport(ctx ReportContext) (string, error) {
	*p.target = ctx.Related("tls_probes")
	return "<p>ok</p>", nil
}

func TestServer_Report_BadBody(t *testing.T) {
	p := &testProvider{
		key:        "test",
		definition: &CheckerDefinition{ID: "test-checker", Rules: []CheckRule{}},
	}
	srv := newTestServer(p)
	req := httptest.NewRequest("POST", "/report", bytes.NewBufferString("{bad"))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /report bad body = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServer_NoDefinition_NoEvaluateEndpoint(t *testing.T) {
	// A provider that does NOT implement CheckerDefinitionProvider
	p := &stubProvider{key: "basic"}
	srv := NewServer(p)
	rec := doRequest(srv.Handler(), "POST", "/evaluate", nil, nil)
	// Should 404 or 405 since /evaluate is not registered
	if rec.Code == http.StatusOK {
		t.Error("POST /evaluate should not be available without CheckerDefinitionProvider")
	}
}
