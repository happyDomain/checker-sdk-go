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

import "context"

type enabledRulesCtxKey struct{}

// WithEnabledRules returns a context carrying the host's per-rule enable map.
// The SDK server attaches it before calling ObservationProvider.Collect so
// providers can skip optional work (network calls, paid API hits, …) for
// rules the host has disabled. A nil map means "run everything".
func WithEnabledRules(ctx context.Context, enabled map[string]bool) context.Context {
	if enabled == nil {
		return ctx
	}
	return context.WithValue(ctx, enabledRulesCtxKey{}, enabled)
}

// EnabledRulesFromContext returns the enabled-rule map attached by
// WithEnabledRules, or nil if none. RuleEnabled is the usual access pattern.
func EnabledRulesFromContext(ctx context.Context) map[string]bool {
	m, _ := ctx.Value(enabledRulesCtxKey{}).(map[string]bool)
	return m
}

// RuleEnabled reports whether ruleName is enabled given the host's map.
// Absent rules default to enabled (nil map or rule not in map), matching
// the SDK server's evaluate-side semantics.
func RuleEnabled(ctx context.Context, ruleName string) bool {
	m := EnabledRulesFromContext(ctx)
	if m == nil {
		return true
	}
	enabled, ok := m[ruleName]
	if !ok {
		return true
	}
	return enabled
}
