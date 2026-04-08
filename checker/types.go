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

// Package checker provides the public types and helpers for writing
// happyDomain checker plugins. It is the stable API surface that all
// external checkers should depend on.
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CheckScopeType represents the scope level of a check target.
type CheckScopeType int

const (
	CheckScopeAdmin CheckScopeType = 0
	CheckScopeUser  CheckScopeType = iota
	CheckScopeDomain
	CheckScopeZone
	CheckScopeService
)

const (
	AutoFillDomainName  = "domain_name"
	AutoFillSubdomain   = "subdomain"
	AutoFillZone        = "zone"
	AutoFillServiceType = "service_type"
	AutoFillService     = "service"
)

// CheckTarget identifies the resource a check applies to. Identifiers are
// passed as opaque strings so the SDK stays self-contained and does not
// depend on any happyDomain-specific identifier type. The host is free to
// parse them into its own representation at the boundary.
type CheckTarget struct {
	UserId      string `json:"userId,omitempty"`
	DomainId    string `json:"domainId,omitempty"`
	ServiceId   string `json:"serviceId,omitempty"`
	ServiceType string `json:"serviceType,omitempty"`
}

// Scope returns the most specific scope level of this target.
func (t CheckTarget) Scope() CheckScopeType {
	if t.ServiceId != "" {
		return CheckScopeService
	}
	if t.DomainId != "" {
		return CheckScopeDomain
	}
	return CheckScopeUser
}

// String returns a stable string representation of the target.
func (t CheckTarget) String() string {
	var parts []string
	if t.UserId != "" {
		parts = append(parts, t.UserId)
	}
	if t.DomainId != "" {
		parts = append(parts, t.DomainId)
	}
	if t.ServiceId != "" {
		parts = append(parts, t.ServiceId)
	}
	return strings.Join(parts, "/")
}

// CheckerAvailability declares on which scopes a checker can operate.
type CheckerAvailability struct {
	ApplyToDomain    bool     `json:"applyToDomain,omitempty"`
	ApplyToZone      bool     `json:"applyToZone,omitempty"`
	ApplyToService   bool     `json:"applyToService,omitempty"`
	LimitToProviders []string `json:"limitToProviders,omitempty"`
	LimitToServices  []string `json:"limitToServices,omitempty"`
}

// CheckerOptions holds the runtime options for a checker execution.
type CheckerOptions map[string]any

// CheckerOptionField describes a single checker option, used to document
// what configuration the checker accepts. The fields mirror happyDomain's
// generic Field type so that the host can re-export it as a type alias and
// keep using its existing form-rendering code unchanged.
type CheckerOptionField struct {
	// Id is the option identifier (the key in CheckerOptions).
	Id string `json:"id" binding:"required"`

	// Type is the string representation of the option's type
	// (e.g. "string", "number", "uint", "bool").
	Type string `json:"type" binding:"required"`

	// Label is the title shown to the user.
	Label string `json:"label,omitempty"`

	// Placeholder is the placeholder shown in the input.
	Placeholder string `json:"placeholder,omitempty"`

	// Default is the value used when the option is not set by the user.
	Default any `json:"default,omitempty"`

	// Choices holds the available choices for a dropdown option.
	Choices []string `json:"choices,omitempty"`

	// Required indicates whether the option must be filled.
	Required bool `json:"required,omitempty"`

	// Secret indicates that the option holds sensitive information
	// (API keys, tokens, …).
	Secret bool `json:"secret,omitempty"`

	// Hide indicates that the option should be hidden from the user.
	Hide bool `json:"hide,omitempty"`

	// Textarea indicates that a multi-line input should be used.
	Textarea bool `json:"textarea,omitempty"`

	// Description is a help sentence describing the option.
	Description string `json:"description,omitempty"`

	// AutoFill indicates that this option is automatically populated by the
	// host based on execution context (e.g. domain name, service payload).
	AutoFill string `json:"autoFill,omitempty"`

	// NoOverride indicates that once this option is set at a given scope,
	// more specific scopes cannot override its value.
	NoOverride bool `json:"noOverride,omitempty"`
}

// CheckerOptionDocumentation describes a single checker option.
type CheckerOptionDocumentation = CheckerOptionField

// CheckerOptionsDocumentation describes all options a checker accepts, organized by level.
type CheckerOptionsDocumentation struct {
	AdminOpts   []CheckerOptionDocumentation `json:"adminOpts,omitempty"`
	UserOpts    []CheckerOptionDocumentation `json:"userOpts,omitempty"`
	DomainOpts  []CheckerOptionDocumentation `json:"domainOpts,omitempty"`
	ServiceOpts []CheckerOptionDocumentation `json:"serviceOpts,omitempty"`
	RunOpts     []CheckerOptionDocumentation `json:"runOpts,omitempty"`
}

// Status represents the result status of a check evaluation.
//
// Numeric ordering is severity ordering: lower = better, higher = worse.
// StatusUnknown is intentionally the zero value, so an uninitialized
// CheckState reads as "no signal yet" rather than as a healthy OK.
// "Good" statuses are negative so that aggregators can simply take the
// max() of a set of statuses to compute the worst one.
type Status int

const (
	StatusOK      Status = -2
	StatusInfo    Status = -1
	StatusUnknown Status = 0 // zero value: not initialized / no signal yet
	StatusWarn    Status = 1
	StatusCrit    Status = 2
	StatusError   Status = 3
)

// String returns the human-readable name of the status.
func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "UNKNOWN"
	case StatusOK:
		return "OK"
	case StatusInfo:
		return "INFO"
	case StatusWarn:
		return "WARN"
	case StatusCrit:
		return "CRIT"
	case StatusError:
		return "ERROR"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// MarshalJSON serializes Status as its string name so the wire format
// is stable across any future reordering of the underlying int values.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON accepts either the string name (preferred) or a raw int
// (for backward compatibility with older clients/snapshots).
func (s *Status) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		switch name {
		case "OK":
			*s = StatusOK
		case "INFO":
			*s = StatusInfo
		case "UNKNOWN", "":
			*s = StatusUnknown
		case "WARN":
			*s = StatusWarn
		case "CRIT":
			*s = StatusCrit
		case "ERROR":
			*s = StatusError
		default:
			return fmt.Errorf("unknown status %q", name)
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*s = Status(n)
	return nil
}

// CheckState is the result of evaluating a single rule.
type CheckState struct {
	Status  Status         `json:"status"`
	Message string         `json:"message"`
	Code    string         `json:"code,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// CheckMetric represents a single metric produced by a check.
type CheckMetric struct {
	Name      string            `json:"name" binding:"required"`
	Value     float64           `json:"value" binding:"required"`
	Unit      string            `json:"unit,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp" binding:"required" format:"date-time"`
}

// ObservationKey identifies a type of observation data.
type ObservationKey = string

// CheckIntervalSpec defines scheduling bounds for a checker.
type CheckIntervalSpec struct {
	Min     time.Duration `json:"min" swaggertype:"integer"`
	Max     time.Duration `json:"max" swaggertype:"integer"`
	Default time.Duration `json:"default" swaggertype:"integer"`
}

// ObservationProvider collects a specific type of data for a target.
type ObservationProvider interface {
	Key() ObservationKey
	Collect(ctx context.Context, opts CheckerOptions) (any, error)
}

// CheckRuleInfo is the JSON-serializable description of a rule, for API/UI listing.
type CheckRuleInfo struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Options     *CheckerOptionsDocumentation `json:"options,omitempty"`
}

// CheckRule evaluates observations and produces a CheckState.
type CheckRule interface {
	Name() string
	Description() string
	Evaluate(ctx context.Context, obs ObservationGetter, opts CheckerOptions) CheckState
}

// CheckRuleWithOptions is an optional interface that rules can implement
// to declare their own options documentation for API/UI grouping.
type CheckRuleWithOptions interface {
	CheckRule
	Options() CheckerOptionsDocumentation
}

// ObservationGetter provides access to observation data (used by CheckRule).
// Get unmarshals observation data into dest (like json.Unmarshal).
type ObservationGetter interface {
	Get(ctx context.Context, key ObservationKey, dest any) error
}

// CheckAggregator combines multiple CheckStates into a single result.
type CheckAggregator interface {
	Aggregate(states []CheckState) CheckState
}

// CheckerHTMLReporter is an optional interface that observation providers can
// implement to render their stored data as a full HTML document (for iframe embedding).
// Detect support with a type assertion: _, ok := provider.(CheckerHTMLReporter)
type CheckerHTMLReporter interface {
	// GetHTMLReport generates an HTML document from the JSON-encoded observation data.
	GetHTMLReport(raw json.RawMessage) (string, error)
}

// CheckerMetricsReporter is an optional interface that observation providers can
// implement to extract time-series metrics from their stored data.
// Detect support with a type assertion: _, ok := provider.(CheckerMetricsReporter)
type CheckerMetricsReporter interface {
	// ExtractMetrics returns metrics from JSON-encoded observation data.
	ExtractMetrics(raw json.RawMessage, collectedAt time.Time) ([]CheckMetric, error)
}

// CheckerDefinitionProvider is an optional interface that observation providers can
// implement to expose their checker definition. Used by the SDK server to serve
// /definition and /evaluate endpoints without requiring a separate argument.
// Detect support with a type assertion: _, ok := provider.(CheckerDefinitionProvider)
type CheckerDefinitionProvider interface {
	// Definition returns the checker definition for this provider.
	Definition() *CheckerDefinition
}

// CheckerDefinition is the complete definition of a checker, registered via init().
type CheckerDefinition struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	Version         string                      `json:"version,omitempty"`
	Availability    CheckerAvailability         `json:"availability"`
	Options         CheckerOptionsDocumentation `json:"options"`
	RulesInfo       []CheckRuleInfo             `json:"rules"`
	Rules           []CheckRule                 `json:"-"`
	Aggregator      CheckAggregator             `json:"-"`
	Interval        *CheckIntervalSpec          `json:"interval,omitempty"`
	HasHTMLReport   bool                        `json:"has_html_report,omitempty"`
	HasMetrics      bool                        `json:"has_metrics,omitempty"`
	ObservationKeys []ObservationKey            `json:"observationKeys,omitempty"`
}

// BuildRulesInfo populates RulesInfo from the Rules slice.
func (d *CheckerDefinition) BuildRulesInfo() {
	d.RulesInfo = make([]CheckRuleInfo, len(d.Rules))
	for i, rule := range d.Rules {
		info := CheckRuleInfo{
			Name:        rule.Name(),
			Description: rule.Description(),
		}
		if rwo, ok := rule.(CheckRuleWithOptions); ok {
			opts := rwo.Options()
			info.Options = &opts
		}
		d.RulesInfo[i] = info
	}
}

// OptionsValidator is an optional interface that checkers (or their rules/providers)
// can implement to perform domain-specific validation of checker options.
type OptionsValidator interface {
	ValidateOptions(opts CheckerOptions) error
}

// ExternalCollectRequest is sent to POST /collect on a remote checker endpoint.
type ExternalCollectRequest struct {
	Key     ObservationKey `json:"key"`
	Target  CheckTarget    `json:"target"`
	Options CheckerOptions `json:"options"`
}

// ExternalCollectResponse is returned by POST /collect on a remote checker endpoint.
type ExternalCollectResponse struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// ExternalEvaluateRequest is sent to POST /evaluate on a remote checker endpoint.
type ExternalEvaluateRequest struct {
	Observations map[ObservationKey]json.RawMessage `json:"observations"`
	Options      CheckerOptions                     `json:"options"`
	EnabledRules map[string]bool                    `json:"enabledRules,omitempty"`
}

// ExternalEvaluateResponse is returned by POST /evaluate on a remote checker endpoint.
type ExternalEvaluateResponse struct {
	States []CheckState `json:"states"`
	Error  string       `json:"error,omitempty"`
}

// ExternalReportRequest is sent to POST /report on a remote checker endpoint.
type ExternalReportRequest struct {
	Key  ObservationKey  `json:"key"`
	Data json.RawMessage `json:"data"`
}
