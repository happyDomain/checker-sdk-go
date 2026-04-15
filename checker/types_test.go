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

func TestStatus_MarshalJSON(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusUnknown, `0`},
		{StatusOK, `1`},
		{StatusInfo, `2`},
		{StatusWarn, `3`},
		{StatusCrit, `4`},
		{StatusError, `5`},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.status)
		if err != nil {
			t.Errorf("Marshal(%v) error: %v", tt.status, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("Marshal(%v) = %s, want %s", tt.status, got, tt.want)
		}
	}
}

func TestStatus_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  Status
	}{
		{`0`, StatusUnknown},
		{`1`, StatusOK},
		{`2`, StatusInfo},
		{`3`, StatusWarn},
		{`4`, StatusCrit},
		{`5`, StatusError},
	}
	for _, tt := range tests {
		var got Status
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Unmarshal(%s) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestStatus_RoundTrip(t *testing.T) {
	for _, s := range []Status{StatusOK, StatusInfo, StatusUnknown, StatusWarn, StatusCrit, StatusError} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal(%v) error: %v", s, err)
		}
		var got Status
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%s) error: %v", data, err)
		}
		if got != s {
			t.Errorf("round-trip %v: got %v", s, got)
		}
	}
}

func TestStatus_String(t *testing.T) {
	if got := StatusOK.String(); got != "OK" {
		t.Errorf("StatusOK.String() = %q, want \"OK\"", got)
	}
	if got := Status(99).String(); got != "Status(99)" {
		t.Errorf("Status(99).String() = %q, want \"Status(99)\"", got)
	}
}

func TestCheckTarget_Scope(t *testing.T) {
	tests := []struct {
		target CheckTarget
		want   CheckScopeType
	}{
		{CheckTarget{}, CheckScopeUser},
		{CheckTarget{UserId: "u1"}, CheckScopeUser},
		{CheckTarget{DomainId: "d1"}, CheckScopeDomain},
		{CheckTarget{DomainId: "d1", ServiceId: "s1"}, CheckScopeService},
		{CheckTarget{ServiceId: "s1"}, CheckScopeService},
	}
	for _, tt := range tests {
		if got := tt.target.Scope(); got != tt.want {
			t.Errorf("%+v.Scope() = %v, want %v", tt.target, got, tt.want)
		}
	}
}

func TestCheckTarget_String(t *testing.T) {
	tests := []struct {
		target CheckTarget
		want   string
	}{
		{CheckTarget{}, "//"},
		{CheckTarget{UserId: "u1"}, "u1//"},
		{CheckTarget{UserId: "u1", DomainId: "d1"}, "u1/d1/"},
		{CheckTarget{UserId: "u1", DomainId: "d1", ServiceId: "s1"}, "u1/d1/s1"},
		// Ensure different targets with different empty fields don't collide.
		{CheckTarget{DomainId: "d1"}, "/d1/"},
		{CheckTarget{ServiceId: "s1"}, "//s1"},
	}
	for _, tt := range tests {
		if got := tt.target.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestCheckerDefinition_BuildRulesInfo(t *testing.T) {
	d := &CheckerDefinition{
		Rules: []CheckRule{&dummyRule{name: "r1", desc: "desc1"}},
	}
	d.BuildRulesInfo()
	if len(d.RulesInfo) != 1 {
		t.Fatalf("BuildRulesInfo: got %d rules, want 1", len(d.RulesInfo))
	}
	if d.RulesInfo[0].Name != "r1" || d.RulesInfo[0].Description != "desc1" {
		t.Errorf("BuildRulesInfo: got %+v, want {Name:r1, Description:desc1}", d.RulesInfo[0])
	}
}

func TestRegisterChecker_EmptyIDRejected(t *testing.T) {
	resetRegistries()
	RegisterChecker(&CheckerDefinition{ID: "", Name: "bad"})
	if len(GetCheckers()) != 0 {
		t.Error("checker with empty ID should not be registered")
	}
}
