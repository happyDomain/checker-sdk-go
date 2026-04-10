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
func (p *testProvider) GetHTMLReport(raw json.RawMessage) (string, error) {
	if p.htmlFn != nil {
		return p.htmlFn(raw)
	}
	return "<h1>report</h1>", nil
}
func (p *testProvider) ExtractMetrics(raw json.RawMessage, t time.Time) ([]CheckMetric, error) {
	if p.metricsFn != nil {
		return p.metricsFn(raw, t)
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
func (r *dummyRule) Evaluate(ctx context.Context, obs ObservationGetter, opts CheckerOptions) CheckState {
	return CheckState{Status: StatusOK, Message: r.name + " passed"}
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
	rec := doRequest(srv.Handler(), "GET", "/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("GET /health status = %q, want \"ok\"", resp["status"])
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
	if resp.States[0].Code != "rule1" {
		t.Errorf("evaluate state[0].Code = %q, want \"rule1\"", resp.States[0].Code)
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
	if resp.States[0].Code != "rule2" {
		t.Errorf("remaining state code = %q, want \"rule2\"", resp.States[0].Code)
	}
}

func TestServer_Report_HTML(t *testing.T) {
	p := &testProvider{
		key: "test",
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
		key: "test",
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
