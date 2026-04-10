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
	"testing"
)

func TestGetOption_DirectType(t *testing.T) {
	opts := CheckerOptions{"key": "hello"}
	got, ok := GetOption[string](opts, "key")
	if !ok || got != "hello" {
		t.Errorf("GetOption[string] = (%q, %v), want (\"hello\", true)", got, ok)
	}
}

func TestGetOption_MissingKey(t *testing.T) {
	opts := CheckerOptions{}
	got, ok := GetOption[string](opts, "missing")
	if ok || got != "" {
		t.Errorf("GetOption[string] missing key = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestGetOption_JSONRoundTrip(t *testing.T) {
	// Simulate what happens when options come from JSON: numbers become float64,
	// structs become map[string]any.
	type inner struct {
		X int    `json:"x"`
		Y string `json:"y"`
	}
	opts := CheckerOptions{
		"obj": map[string]any{"x": float64(42), "y": "test"},
	}
	got, ok := GetOption[inner](opts, "obj")
	if !ok {
		t.Fatal("GetOption[inner] returned false")
	}
	if got.X != 42 || got.Y != "test" {
		t.Errorf("GetOption[inner] = %+v, want {X:42 Y:test}", got)
	}
}

func TestGetOption_WrongType(t *testing.T) {
	opts := CheckerOptions{"key": 123}
	got, ok := GetOption[string](opts, "key")
	// int 123 cannot be unmarshaled into string via JSON
	if ok {
		t.Errorf("GetOption[string] with int value = (%q, true), want (\"\", false)", got)
	}
}

func TestGetFloatOption_NativeFloat(t *testing.T) {
	opts := CheckerOptions{"f": 3.14}
	if got := GetFloatOption(opts, "f", 0); got != 3.14 {
		t.Errorf("GetFloatOption = %v, want 3.14", got)
	}
}

func TestGetFloatOption_JSONNumber(t *testing.T) {
	opts := CheckerOptions{"f": json.Number("2.718")}
	if got := GetFloatOption(opts, "f", 0); got != 2.718 {
		t.Errorf("GetFloatOption(json.Number) = %v, want 2.718", got)
	}
}

func TestGetFloatOption_Missing(t *testing.T) {
	opts := CheckerOptions{}
	if got := GetFloatOption(opts, "f", 99.9); got != 99.9 {
		t.Errorf("GetFloatOption missing = %v, want 99.9", got)
	}
}

func TestGetFloatOption_WrongType(t *testing.T) {
	opts := CheckerOptions{"f": "not a number"}
	if got := GetFloatOption(opts, "f", 1.0); got != 1.0 {
		t.Errorf("GetFloatOption wrong type = %v, want 1.0", got)
	}
}

func TestGetIntOption(t *testing.T) {
	opts := CheckerOptions{"i": float64(42)}
	if got := GetIntOption(opts, "i", 0); got != 42 {
		t.Errorf("GetIntOption = %v, want 42", got)
	}
}

func TestGetIntOption_Missing(t *testing.T) {
	opts := CheckerOptions{}
	if got := GetIntOption(opts, "i", 10); got != 10 {
		t.Errorf("GetIntOption missing = %v, want 10", got)
	}
}

func TestGetBoolOption(t *testing.T) {
	opts := CheckerOptions{"b": true}
	if got := GetBoolOption(opts, "b", false); got != true {
		t.Errorf("GetBoolOption = %v, want true", got)
	}
}

func TestGetBoolOption_Missing(t *testing.T) {
	opts := CheckerOptions{}
	if got := GetBoolOption(opts, "b", true); got != true {
		t.Errorf("GetBoolOption missing = %v, want true", got)
	}
}

func TestGetBoolOption_WrongType(t *testing.T) {
	opts := CheckerOptions{"b": "yes"}
	if got := GetBoolOption(opts, "b", false); got != false {
		t.Errorf("GetBoolOption wrong type = %v, want false", got)
	}
}
