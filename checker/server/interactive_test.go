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

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"git.happydns.org/checker-sdk-go/checker"
)

// interactiveProvider embeds testProvider and adds Interactive.
type interactiveProvider struct {
	*testProvider
	fields   []checker.CheckerOptionField
	parseFn  func(r *http.Request) (checker.CheckerOptions, error)
	parseErr error
}

func (p *interactiveProvider) RenderForm() []checker.CheckerOptionField {
	return p.fields
}

func (p *interactiveProvider) ParseForm(r *http.Request) (checker.CheckerOptions, error) {
	if p.parseErr != nil {
		return nil, p.parseErr
	}
	if p.parseFn != nil {
		return p.parseFn(r)
	}
	return checker.CheckerOptions{"domain": r.FormValue("domain")}, nil
}

func postForm(handler http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// minimalProvider implements only ObservationProvider.
type minimalProvider struct{ key checker.ObservationKey }

func (m *minimalProvider) Key() checker.ObservationKey { return m.key }
func (m *minimalProvider) Collect(ctx context.Context, opts checker.CheckerOptions) (any, error) {
	return nil, nil
}

func TestCheck_NotRegistered_WhenProviderLacksInterface(t *testing.T) {
	p := &minimalProvider{key: "test"}
	srv := New(p)
	defer srv.Close()

	rec := doRequest(srv.Handler(), "GET", "/check", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /check without Interactive = %d, want 404", rec.Code)
	}
}

func TestCheck_Form_Renders(t *testing.T) {
	p := &interactiveProvider{
		testProvider: &testProvider{key: "test"},
		fields: []checker.CheckerOptionField{
			{Id: "domain", Type: "string", Label: "Domain name", Required: true, Placeholder: "example.com"},
			{Id: "verbose", Type: "bool", Label: "Verbose", Default: true},
			{Id: "flavor", Type: "string", Choices: []string{"a", "b"}, Default: "b"},
			{Id: "hidden", Type: "string", Hide: true},
		},
	}
	srv := New(p)
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
	definition := &checker.CheckerDefinition{
		ID:   "test",
		Name: "Test Checker",
		Rules: []checker.CheckRule{
			&dummyRule{name: "rule1", desc: "first rule"},
		},
	}
	p := &interactiveProvider{
		testProvider: &testProvider{key: "test", definition: definition},
		fields:       []checker.CheckerOptionField{{Id: "domain", Type: "string"}},
	}
	srv := New(p)
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
		fields:       []checker.CheckerOptionField{{Id: "domain", Type: "string"}},
		parseErr:     errors.New("domain is required"),
	}
	srv := New(p)
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
			collectFn: func(ctx context.Context, opts checker.CheckerOptions) (any, error) {
				return nil, errors.New("boom")
			},
		},
		fields: []checker.CheckerOptionField{{Id: "domain", Type: "string"}},
	}
	srv := New(p)
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
	// Provider implements Interactive and has a definition (so
	// /evaluate-like logic runs) but no HTMLReporter / MetricsReporter.
	bare := &bareInteractiveProvider{
		key: "test",
		def: &checker.CheckerDefinition{
			ID:    "test",
			Rules: []checker.CheckRule{&dummyRule{name: "r", desc: "r"}},
		},
	}
	srv := New(bare)
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
// (ObservationProvider, CheckerDefinitionProvider, Interactive),
// no reporters.
type bareInteractiveProvider struct {
	key checker.ObservationKey
	def *checker.CheckerDefinition
}

func (b *bareInteractiveProvider) Key() checker.ObservationKey { return b.key }
func (b *bareInteractiveProvider) Collect(ctx context.Context, opts checker.CheckerOptions) (any, error) {
	return map[string]string{"ok": "1"}, nil
}
func (b *bareInteractiveProvider) Definition() *checker.CheckerDefinition { return b.def }
func (b *bareInteractiveProvider) RenderForm() []checker.CheckerOptionField {
	return []checker.CheckerOptionField{{Id: "domain", Type: "string"}}
}
func (b *bareInteractiveProvider) ParseForm(r *http.Request) (checker.CheckerOptions, error) {
	return checker.CheckerOptions{"domain": r.FormValue("domain")}, nil
}

type siblingProvider struct {
	key        checker.ObservationKey
	id         string
	entriesOpt string
	gotOpts    checker.CheckerOptions
	payload    any
}

func (s *siblingProvider) Key() checker.ObservationKey { return s.key }
func (s *siblingProvider) Collect(ctx context.Context, opts checker.CheckerOptions) (any, error) {
	s.gotOpts = opts
	return s.payload, nil
}
func (s *siblingProvider) Definition() *checker.CheckerDefinition {
	return &checker.CheckerDefinition{
		ID: s.id,
		Options: checker.CheckerOptionsDocumentation{
			RunOpts: []checker.CheckerOptionDocumentation{
				{Id: s.entriesOpt, Type: "array", AutoFill: checker.AutoFillDiscoveryEntries},
			},
		},
	}
}

type primaryWithSibling struct {
	key     checker.ObservationKey
	def     *checker.CheckerDefinition
	entries []checker.DiscoveryEntry
	sibling checker.ObservationProvider
}

func (p *primaryWithSibling) Key() checker.ObservationKey { return p.key }
func (p *primaryWithSibling) Collect(ctx context.Context, opts checker.CheckerOptions) (any, error) {
	return map[string]string{"primary": "ok"}, nil
}
func (p *primaryWithSibling) Definition() *checker.CheckerDefinition { return p.def }
func (p *primaryWithSibling) RenderForm() []checker.CheckerOptionField {
	return []checker.CheckerOptionField{{Id: "domain", Type: "string"}}
}
func (p *primaryWithSibling) ParseForm(r *http.Request) (checker.CheckerOptions, error) {
	return checker.CheckerOptions{"domain": r.FormValue("domain")}, nil
}
func (p *primaryWithSibling) DiscoverEntries(data any) ([]checker.DiscoveryEntry, error) {
	return p.entries, nil
}
func (p *primaryWithSibling) RelatedProviders() []checker.ObservationProvider {
	return []checker.ObservationProvider{p.sibling}
}

type relatedAssertRule struct {
	key checker.ObservationKey
}

func (r *relatedAssertRule) Name() string        { return "related_assert" }
func (r *relatedAssertRule) Description() string { return "" }
func (r *relatedAssertRule) Evaluate(ctx context.Context, obs checker.ObservationGetter, opts checker.CheckerOptions) []checker.CheckState {
	related, err := obs.GetRelated(ctx, r.key)
	if err != nil {
		return []checker.CheckState{{Status: checker.StatusError, Message: err.Error()}}
	}
	if len(related) == 0 {
		return []checker.CheckState{{Status: checker.StatusCrit, Message: "no related observation"}}
	}
	return []checker.CheckState{{Status: checker.StatusOK, Message: "saw related observation"}}
}

func TestCheck_Submit_RunsSiblingAndExposesRelated(t *testing.T) {
	sibling := &siblingProvider{
		key:        "sibling_key",
		id:         "sibling",
		entriesOpt: "endpoints",
		payload:    map[string]string{"sibling": "ok"},
	}
	entry := checker.DiscoveryEntry{Type: "fake.v1", Ref: "r1"}
	primary := &primaryWithSibling{
		key: "primary_key",
		def: &checker.CheckerDefinition{
			ID:    "primary",
			Rules: []checker.CheckRule{&relatedAssertRule{key: sibling.key}},
		},
		entries: []checker.DiscoveryEntry{entry},
		sibling: sibling,
	}

	srv := New(primary)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "saw related observation") {
		t.Errorf("rule did not see related observation; body:\n%s", body)
	}

	got, ok := sibling.gotOpts[sibling.entriesOpt].([]checker.DiscoveryEntry)
	if !ok {
		t.Fatalf("sibling opts missing %q or wrong type: %#v", sibling.entriesOpt, sibling.gotOpts[sibling.entriesOpt])
	}
	if len(got) != 1 || got[0].Ref != entry.Ref {
		t.Errorf("sibling saw entries %v, want [%v]", got, entry)
	}

	if v, _ := sibling.gotOpts["domain"].(string); v != "example.com" {
		t.Errorf("sibling did not receive primary domain opt, got %q", v)
	}
}

// interactiveStatesPeekingProvider implements Interactive + HTMLReporter
// and captures the ReportContext.States() seen at GetHTMLReport time.
type interactiveStatesPeekingProvider struct {
	key  checker.ObservationKey
	def  *checker.CheckerDefinition
	seen *[]checker.CheckState
}

func (p *interactiveStatesPeekingProvider) Key() checker.ObservationKey { return p.key }
func (p *interactiveStatesPeekingProvider) Collect(ctx context.Context, opts checker.CheckerOptions) (any, error) {
	return map[string]string{"ok": "1"}, nil
}
func (p *interactiveStatesPeekingProvider) Definition() *checker.CheckerDefinition { return p.def }
func (p *interactiveStatesPeekingProvider) RenderForm() []checker.CheckerOptionField {
	return []checker.CheckerOptionField{{Id: "domain", Type: "string"}}
}
func (p *interactiveStatesPeekingProvider) ParseForm(r *http.Request) (checker.CheckerOptions, error) {
	return checker.CheckerOptions{"domain": r.FormValue("domain")}, nil
}
func (p *interactiveStatesPeekingProvider) GetHTMLReport(ctx checker.ReportContext) (string, error) {
	if p.seen != nil {
		*p.seen = ctx.States()
	}
	return "<p>ok</p>", nil
}

// TestCheck_Submit_ThreadsStatesIntoReport verifies that CheckStates
// produced by evaluateRules during POST /check are threaded into the
// ReportContext handed to GetHTMLReport. Without this wiring, the /check
// UI can show states in its own section but the embedded report would
// have to re-derive severity/hints from Data.
func TestCheck_Submit_ThreadsStatesIntoReport(t *testing.T) {
	var seen []checker.CheckState
	p := &interactiveStatesPeekingProvider{
		key: "test",
		def: &checker.CheckerDefinition{
			ID:    "test",
			Rules: []checker.CheckRule{&dummyRule{name: "rule1", desc: "first"}},
		},
		seen: &seen,
	}
	srv := New(p)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200", rec.Code)
	}
	if len(seen) != 1 {
		t.Fatalf("reporter saw %d states, want 1", len(seen))
	}
	if seen[0].RuleName != "rule1" {
		t.Errorf("state RuleName = %q, want %q", seen[0].RuleName, "rule1")
	}
	if seen[0].Status != checker.StatusOK {
		t.Errorf("state Status = %v, want %v", seen[0].Status, checker.StatusOK)
	}
}

func TestCheck_Submit_NoSibling_LeavesRelatedEmpty(t *testing.T) {
	p := &interactiveProvider{
		testProvider: &testProvider{
			key: "test",
			definition: &checker.CheckerDefinition{
				ID:    "test",
				Rules: []checker.CheckRule{&relatedAssertRule{key: "other"}},
			},
		},
		fields: []checker.CheckerOptionField{{Id: "domain", Type: "string"}},
	}
	srv := New(p)
	defer srv.Close()

	rec := postForm(srv.Handler(), "/check", url.Values{"domain": {"example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no related observation") {
		t.Errorf("rule should have seen no related observation; body:\n%s", body)
	}
}
