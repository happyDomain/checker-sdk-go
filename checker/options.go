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
	"encoding/json"
)

// GetOption extracts a typed value from checker options, handling both
// native Go types (in-process providers) and map[string]any values
// (from JSON round-tripping through HTTP providers). Returns the zero
// value and false if the key is missing or the value cannot be converted.
func GetOption[T any](options CheckerOptions, key string) (T, bool) {
	v, ok := options[key]
	if !ok {
		var zero T
		return zero, false
	}

	// Direct type assertion (in-process path).
	if t, ok := v.(T); ok {
		return t, true
	}

	// JSON round-trip for values deserialized as map[string]any over HTTP.
	raw, err := json.Marshal(v)
	if err != nil {
		var zero T
		return zero, false
	}
	var t T
	if err := json.Unmarshal(raw, &t); err != nil {
		var zero T
		return zero, false
	}
	return t, true
}

// GetFloatOption extracts a float64 from checker options, handling both
// native float64 values and json.Number. Returns defaultVal if the key
// is missing or the value cannot be converted.
func GetFloatOption(options CheckerOptions, key string, defaultVal float64) float64 {
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return val
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return defaultVal
		}
		return f
	default:
		return defaultVal
	}
}

// GetIntOption extracts an int from checker options, using GetFloatOption
// internally. Returns defaultVal if the key is missing or invalid.
func GetIntOption(options CheckerOptions, key string, defaultVal int) int {
	return int(GetFloatOption(options, key, float64(defaultVal)))
}

// GetBoolOption extracts a bool from checker options.
// Returns defaultVal if the key is missing or the value is not a bool.
func GetBoolOption(options CheckerOptions, key string, defaultVal bool) bool {
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}
