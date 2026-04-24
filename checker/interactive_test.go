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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// interactiveProvider embeds testProvider and adds CheckerInteractive.
type interactiveProvider struct {
	*testProvider
	fields   []CheckerOptionField
	parseFn  func(r *http.Request) (CheckerOptions, error)
	parseErr error
}

func (p *interactiveProvider) RenderForm() []CheckerOptionField {
	return p.fields
}

func (p *interactiveProvider) ParseForm(r *http.Request) (CheckerOptions, error) {
	if p.parseErr != nil {
		return nil, p.parseErr
	}
	if p.parseFn != nil {
		return p.parseFn(r)
	}
	return CheckerOptions{"domain": r.FormValue("domain")}, nil
}

func postForm(handler http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// minimalProvider implements only ObservationProvider.
type minimalProvider struct{ key ObservationKey }

func (m *minimalProvider) Key() ObservationKey { return m.key }
func (m *minimalProvider) Collect(ctx context.Context, opts CheckerOptions) (any, error) {
	return nil, nil
}

func TestCheck_NotRegistered_WhenProviderLacksInterface(t *testing.T) {
	p := &minimalProvider{key: "test"}
	srv := NewServer(p)
	defer srv.Close()

	rec := doRequest(srv.Handler(), "GET", "/check", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /check without CheckerInteractive = %d, want 404", rec.Code)
	}
}

func TestCheck_Form_Renders(t *testing.T) {
	p := &interactiveProvider{
		testProvider: &testProvider{key: "test"},
		fields: []CheckerOptionField{
			{Id: "domain", Type: "string", Label: "Domain name", Required: true, Placeholder: "example.com"},
			{Id: "verbose", Type: "bool", Label: "Verbose", Default: true},
			{Id: "flavor", Type: "string", Choices: []string{"a", "b"}, Default: "b"},
			{Id: "hidden", Type: "string", Hide: true},
		},
	}
	srv := NewServer(p)
	defer srv.Close()

	rec := doRequest(srv.Handler(), "GET", "/check", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /check = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="domain"`,
		`placeholder="example.com"`,
		`Domain name`,
		`type="checkbox"`,
		`name="verbose"`,
		` checked`,
		`<select id="flavor"`,
		`<option value="b" selected>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form body missing %q", want)
		}
	}
	if strings.Contains(body, `name="hidden"`) {
		t.Errorf("hidden field should not be rendered")
	}
}

func TestCheck_Submit_Success(t *testing.T) {
	definition := &CheckerDefinition{
		ID:   "test",
		Name: "Test Checker",
		Rules: []CheckRule{
			&dummyRule{name: "rule1", desc: "first rule"},
		},
	}
	p := &interactiveProvider{
		testProvider: &testProvider{key: "test", definition: definition},
		fields:       []CheckerOptionField{{Id: "domain", Type: "string"}},
	}
	srv := NewServer(p)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`Test Checker`,
		`Check states`,
		`rule1`,
		`rule1 passed`,
		`badge ok`,
		`Metrics`,
		`m1`,
		`Report`,
		`<iframe`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("result body missing %q", want)
		}
	}
}

func TestCheck_Submit_ParseError_RerendersForm(t *testing.T) {
	p := &interactiveProvider{
		testProvider: &testProvider{key: "test"},
		fields:       []CheckerOptionField{{Id: "domain", Type: "string"}},
		parseErr:     errors.New("domain is required"),
	}
	srv := NewServer(p)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {""}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /check with bad input = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "domain is required") {
		t.Errorf("body missing error message, got: %s", body)
	}
	if !strings.Contains(body, `name="domain"`) {
		t.Errorf("form not re-rendered on error")
	}
}

func TestCheck_Submit_CollectError(t *testing.T) {
	p := &interactiveProvider{
		testProvider: &testProvider{
			key: "test",
			collectFn: func(ctx context.Context, opts CheckerOptions) (any, error) {
				return nil, errors.New("boom")
			},
		},
		fields: []CheckerOptionField{{Id: "domain", Type: "string"}},
	}
	srv := NewServer(p)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"x"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200 (collect failure still renders a page)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Collect failed") || !strings.Contains(body, "boom") {
		t.Errorf("body missing Collect error, got: %s", body)
	}
	if strings.Contains(body, "Check states") {
		t.Errorf("states section should not render when Collect failed")
	}
}

func TestCheck_NoReporters(t *testing.T) {
	// Provider implements CheckerInteractive and has a definition (so
	// /evaluate-like logic runs) but no HTMLReporter / MetricsReporter.
	bare := &bareInteractiveProvider{
		key: "test",
		def: &CheckerDefinition{
			ID:    "test",
			Rules: []CheckRule{&dummyRule{name: "r", desc: "r"}},
		},
	}
	srv := NewServer(bare)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"x"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Check states") {
		t.Errorf("body missing states section")
	}
	if strings.Contains(body, "<iframe") {
		t.Errorf("body should not contain iframe when no HTML reporter")
	}
	if strings.Contains(body, "<h2>Metrics</h2>") {
		t.Errorf("body should not contain metrics section when no metrics reporter")
	}
}

// bareInteractiveProvider implements only the required interfaces
// (ObservationProvider, CheckerDefinitionProvider, CheckerInteractive)
//, no reporters.
type bareInteractiveProvider struct {
	key ObservationKey
	def *CheckerDefinition
}

func (b *bareInteractiveProvider) Key() ObservationKey { return b.key }
func (b *bareInteractiveProvider) Collect(ctx context.Context, opts CheckerOptions) (any, error) {
	return map[string]string{"ok": "1"}, nil
}
func (b *bareInteractiveProvider) Definition() *CheckerDefinition { return b.def }
func (b *bareInteractiveProvider) RenderForm() []CheckerOptionField {
	return []CheckerOptionField{{Id: "domain", Type: "string"}}
}
func (b *bareInteractiveProvider) ParseForm(r *http.Request) (CheckerOptions, error) {
	return CheckerOptions{"domain": r.FormValue("domain")}, nil
}
