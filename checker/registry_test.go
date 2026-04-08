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
	"testing"
)

// resetRegistries clears the global registries between tests so that one
// test's registration cannot leak into the next. The package-level maps are
// the only shared state.
func resetRegistries() {
	checkerRegistry = map[string]*CheckerDefinition{}
	observationProviderRegistry = map[ObservationKey]ObservationProvider{}
}

type stubProvider struct {
	key ObservationKey
}

func (s stubProvider) Key() ObservationKey { return s.key }
func (s stubProvider) Collect(ctx context.Context, opts CheckerOptions) (any, error) {
	return nil, nil
}

func TestRegisterChecker_DuplicateIgnored(t *testing.T) {
	resetRegistries()

	first := &CheckerDefinition{ID: "dup", Name: "First"}
	second := &CheckerDefinition{ID: "dup", Name: "Second"}

	RegisterChecker(first)
	RegisterChecker(second)

	got := FindChecker("dup")
	if got == nil {
		t.Fatal("expected checker to remain registered after duplicate")
	}
	if got.Name != "First" {
		t.Errorf("duplicate registration overwrote original: got Name=%q, want %q", got.Name, "First")
	}
	if len(GetCheckers()) != 1 {
		t.Errorf("registry has %d entries, want 1", len(GetCheckers()))
	}
}

func TestRegisterExternalizableChecker_AppendsEndpointOnce(t *testing.T) {
	resetRegistries()

	c := &CheckerDefinition{ID: "ext", Name: "Ext"}

	RegisterExternalizableChecker(c)
	if n := len(c.Options.AdminOpts); n != 1 {
		t.Fatalf("first registration: AdminOpts has %d entries, want 1", n)
	}
	if c.Options.AdminOpts[0].Id != "endpoint" {
		t.Errorf("expected first AdminOpt id %q, got %q", "endpoint", c.Options.AdminOpts[0].Id)
	}

	// Second registration of the same definition pointer must NOT append a
	// second "endpoint" AdminOpt — the duplicate check has to fire before
	// the append, otherwise we silently mutate the live definition.
	RegisterExternalizableChecker(c)
	if n := len(c.Options.AdminOpts); n != 1 {
		t.Errorf("after duplicate registration: AdminOpts has %d entries, want 1", n)
	}
}

func TestRegisterExternalizableChecker_DuplicateDifferentPointerIgnored(t *testing.T) {
	resetRegistries()

	first := &CheckerDefinition{ID: "ext", Name: "First"}
	second := &CheckerDefinition{ID: "ext", Name: "Second"}

	RegisterExternalizableChecker(first)
	RegisterExternalizableChecker(second)

	got := FindChecker("ext")
	if got == nil || got.Name != "First" {
		t.Errorf("expected first registration to win, got %+v", got)
	}
	// The rejected second definition must not have been mutated either.
	if len(second.Options.AdminOpts) != 0 {
		t.Errorf("rejected definition was mutated: AdminOpts=%+v", second.Options.AdminOpts)
	}
}

func TestRegisterObservationProvider_DuplicateIgnored(t *testing.T) {
	resetRegistries()

	first := stubProvider{key: "dns.A"}
	second := stubProvider{key: "dns.A"}

	RegisterObservationProvider(first)
	RegisterObservationProvider(second)

	got := FindObservationProvider("dns.A")
	if got == nil {
		t.Fatal("expected observation provider to remain registered after duplicate")
	}
	// Identity check: the registry must still hold the first instance.
	if _, ok := got.(stubProvider); !ok {
		t.Fatalf("unexpected provider type %T", got)
	}
	if got != ObservationProvider(first) {
		// Both stubProvider values compare equal by value, so this also
		// guards against an accidental overwrite-then-restore pattern.
		t.Errorf("registry no longer holds the first registration")
	}
	if len(GetObservationProviders()) != 1 {
		t.Errorf("registry has %d entries, want 1", len(GetObservationProviders()))
	}
}
