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
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

// CheckerInteractive is an optional interface that observation providers
// can implement to expose a human-facing web form usable standalone,
// outside of a happyDomain host. Detect support with a type assertion:
// _, ok := provider.(CheckerInteractive).
//
// When the provider implements it, Server binds GET and POST on /check.
// GET renders an HTML form built from RenderForm(). POST calls ParseForm
// to obtain the CheckerOptions, then runs the standard pipeline
// (Collect, Evaluate, GetHTMLReport, ExtractMetrics) and renders a
// consolidated result page.
//
// Unlike /evaluate, which relies on happyDomain to fill AutoFill-backed
// options from execution context, a CheckerInteractive implementation is
// responsible for resolving whatever it needs from the human inputs
// (typically via direct DNS queries) before Collect runs.
type CheckerInteractive interface {
	// RenderForm returns the fields the human must fill in to bootstrap
	// a check. Typically a minimal set (domain name, nameserver to
	// query, …) that ParseForm expands into the full CheckerOptions
	// that Collect expects.
	RenderForm() []CheckerOptionField

	// ParseForm reads the submitted form and returns the CheckerOptions
	// ready to feed Collect. It is the checker's responsibility to do
	// whatever lookups or resolutions are needed to populate fields
	// that would normally be auto-filled by happyDomain. Returning an
	// error causes the SDK to re-render the form with the error
	// displayed.
	ParseForm(r *http.Request) (CheckerOptions, error)
}

// checkResult holds everything the result page needs to render.
type checkResult struct {
	Title      string
	States     []CheckState
	Metrics    []CheckMetric
	ReportHTML string
	CollectErr string
	ReportErr  string
	MetricsErr string
}

type checkFormPage struct {
	Title  string
	Fields []CheckerOptionField
	Error  string
}

func (s *Server) handleCheckForm(w http.ResponseWriter, r *http.Request) {
	s.renderCheckForm(w, s.interactive.RenderForm(), "")
}

func (s *Server) handleCheckSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderCheckForm(w, s.interactive.RenderForm(), fmt.Sprintf("invalid form: %v", err))
		return
	}

	opts, err := s.interactive.ParseForm(r)
	if err != nil {
		s.renderCheckForm(w, s.interactive.RenderForm(), err.Error())
		return
	}

	result := &checkResult{Title: s.checkPageTitle()}

	data, err := s.provider.Collect(r.Context(), opts)
	if err != nil {
		result.CollectErr = err.Error()
		s.renderCheckResult(w, result)
		return
	}

	raw, err := json.Marshal(data)
	if err != nil {
		result.CollectErr = fmt.Sprintf("failed to marshal collected data: %v", err)
		s.renderCheckResult(w, result)
		return
	}

	if s.definition != nil {
		obs := &mapObservationGetter{data: map[ObservationKey]json.RawMessage{
			s.provider.Key(): raw,
		}}
		result.States = s.evaluateRules(r.Context(), obs, opts, nil)
	}

	ctx := NewReportContext(raw, nil)

	if reporter, ok := s.provider.(CheckerHTMLReporter); ok {
		html, rerr := reporter.GetHTMLReport(ctx)
		if rerr != nil {
			result.ReportErr = rerr.Error()
		} else {
			result.ReportHTML = html
		}
	}

	if reporter, ok := s.provider.(CheckerMetricsReporter); ok {
		metrics, merr := reporter.ExtractMetrics(ctx, time.Now())
		if merr != nil {
			result.MetricsErr = merr.Error()
		} else {
			result.Metrics = metrics
		}
	}

	s.renderCheckResult(w, result)
}

func (s *Server) checkPageTitle() string {
	if s.definition != nil && s.definition.Name != "" {
		return s.definition.Name
	}
	return "Checker"
}

func renderHTML(w http.ResponseWriter, status int, tpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("render %s: %v", tpl.Name(), err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (s *Server) renderCheckForm(w http.ResponseWriter, fields []CheckerOptionField, errMsg string) {
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusBadRequest
	}
	renderHTML(w, status, checkFormTemplate, checkFormPage{
		Title:  s.checkPageTitle(),
		Fields: fields,
		Error:  errMsg,
	})
}

func (s *Server) renderCheckResult(w http.ResponseWriter, result *checkResult) {
	renderHTML(w, http.StatusOK, checkResultTemplate, result)
}

func statusClass(s Status) string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusInfo:
		return "info"
	case StatusWarn:
		return "warn"
	case StatusCrit:
		return "crit"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// defaultString avoids printing the literal "<nil>" for unset defaults.
func defaultString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func defaultBool(v any) bool {
	b, _ := v.(bool)
	return b
}

var templateFuncs = template.FuncMap{
	"statusClass":   statusClass,
	"statusString":  Status.String,
	"defaultString": defaultString,
	"defaultBool":   defaultBool,
}

const baseCSS = `
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; max-width: 960px; margin: 2rem auto; padding: 0 1rem; color: #222; }
h1, h2 { border-bottom: 1px solid #eee; padding-bottom: 0.3rem; }
form { display: grid; gap: 1rem; }
label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
.required::after { content: " *"; color: #c00; }
.desc { font-weight: normal; color: #666; font-size: 0.9rem; display: block; margin-top: 0.1rem; }
input[type=text], input[type=password], input[type=number], select, textarea {
  width: 100%; padding: 0.5rem; border: 1px solid #bbb; border-radius: 4px; box-sizing: border-box; font: inherit;
}
textarea { min-height: 6rem; }
button { padding: 0.6rem 1.2rem; background: #0b63c5; color: #fff; border: 0; border-radius: 4px; font: inherit; cursor: pointer; }
button:hover { background: #084c98; }
.err { background: #fee; border: 1px solid #fbb; color: #900; padding: 0.6rem 0.8rem; border-radius: 4px; margin: 1rem 0; }
table { border-collapse: collapse; width: 100%; margin: 0.5rem 0 1.5rem; }
th, td { text-align: left; padding: 0.5rem 0.6rem; border-bottom: 1px solid #eee; vertical-align: top; }
th { background: #f7f7f7; }
.badge { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 3px; font-size: 0.8rem; font-weight: 600; color: #fff; }
.badge.ok { background: #2a9d3c; }
.badge.info { background: #3277cc; }
.badge.warn { background: #d08a00; }
.badge.crit { background: #c0392b; }
.badge.error { background: #7a1f1f; }
.badge.unknown { background: #777; }
iframe.report { width: 100%; min-height: 480px; border: 1px solid #ccc; border-radius: 4px; }
.actions { margin-top: 1.5rem; }
.actions a { color: #0b63c5; text-decoration: none; }
.actions a:hover { text-decoration: underline; }
`

var checkFormTemplate = template.Must(template.New("form").Funcs(templateFuncs).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}} – Check</title>
<style>` + baseCSS + `</style>
</head>
<body>
<h1>{{.Title}}</h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<form method="POST" action="/check">
{{range .Fields}}{{if not .Hide}}
<div>
  <label for="{{.Id}}" class="{{if .Required}}required{{end}}">{{if .Label}}{{.Label}}{{else}}{{.Id}}{{end}}
    {{if .Description}}<span class="desc">{{.Description}}</span>{{end}}
  </label>
  {{if .Choices}}
    <select id="{{.Id}}" name="{{.Id}}"{{if .Required}} required{{end}}>
      {{$def := defaultString .Default}}
      {{range .Choices}}<option value="{{.}}"{{if eq . $def}} selected{{end}}>{{.}}</option>{{end}}
    </select>
  {{else if eq .Type "bool"}}
    <input type="checkbox" id="{{.Id}}" name="{{.Id}}" value="true"{{if defaultBool .Default}} checked{{end}}>
  {{else if .Textarea}}
    <textarea id="{{.Id}}" name="{{.Id}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>{{defaultString .Default}}</textarea>
  {{else if eq .Type "number"}}
    <input type="number" step="any" id="{{.Id}}" name="{{.Id}}" placeholder="{{.Placeholder}}" value="{{defaultString .Default}}"{{if .Required}} required{{end}}>
  {{else if eq .Type "uint"}}
    <input type="number" min="0" step="1" id="{{.Id}}" name="{{.Id}}" placeholder="{{.Placeholder}}" value="{{defaultString .Default}}"{{if .Required}} required{{end}}>
  {{else if .Secret}}
    <input type="password" id="{{.Id}}" name="{{.Id}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>
  {{else}}
    <input type="text" id="{{.Id}}" name="{{.Id}}" placeholder="{{.Placeholder}}" value="{{defaultString .Default}}"{{if .Required}} required{{end}}>
  {{end}}
</div>
{{end}}{{end}}
<div><button type="submit">Run check</button></div>
</form>
</body>
</html>`))

var checkResultTemplate = template.Must(template.New("result").Funcs(templateFuncs).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}} – Result</title>
<style>` + baseCSS + `</style>
</head>
<body>
<h1>{{.Title}}</h1>

{{if .CollectErr}}<div class="err"><strong>Collect failed:</strong> {{.CollectErr}}</div>{{end}}

{{if .States}}
<h2>Check states</h2>
<table>
  <thead><tr><th>Status</th><th>Code</th><th>Subject</th><th>Message</th></tr></thead>
  <tbody>
  {{range .States}}
    <tr>
      <td><span class="badge {{statusClass .Status}}">{{statusString .Status}}</span></td>
      <td>{{.Code}}</td>
      <td>{{.Subject}}</td>
      <td>{{.Message}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .Metrics}}
<h2>Metrics</h2>
<table>
  <thead><tr><th>Name</th><th>Value</th><th>Unit</th><th>Labels</th></tr></thead>
  <tbody>
  {{range .Metrics}}
    <tr>
      <td>{{.Name}}</td>
      <td>{{.Value}}</td>
      <td>{{.Unit}}</td>
      <td>{{range $k, $v := .Labels}}{{$k}}={{$v}} {{end}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .MetricsErr}}<div class="err"><strong>Metrics error:</strong> {{.MetricsErr}}</div>{{end}}

{{if .ReportHTML}}
<h2>Report</h2>
<iframe class="report" sandbox srcdoc="{{.ReportHTML}}"></iframe>
{{end}}

{{if .ReportErr}}<div class="err"><strong>Report error:</strong> {{.ReportErr}}</div>{{end}}

<div class="actions"><a href="/check">← Run another check</a></div>
</body>
</html>`))
