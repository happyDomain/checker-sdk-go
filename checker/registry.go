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
	"log"
)

// checkerRegistry is the global registry for checker definitions.
// Thread-safety: all writes happen during init() before any goroutines start.
// After initialization, the map is read-only and safe for concurrent access.
var checkerRegistry = map[string]*CheckerDefinition{}

// observationProviderRegistry is the global registry for observation providers,
// keyed by ObservationKey.
var observationProviderRegistry = map[ObservationKey]ObservationProvider{}

// RegisterChecker registers a checker definition globally. A second
// registration under the same ID is refused with a warning rather than
// silently overwriting the previous entry: in production this almost
// always indicates a deployment mistake (two plugins shipping the same
// checker, or a plugin shadowing a built-in).
func RegisterChecker(c *CheckerDefinition) {
	if c.ID == "" {
		log.Println("Warning: refusing to register checker with empty ID")
		return
	}
	if _, exists := checkerRegistry[c.ID]; exists {
		log.Printf("Warning: checker %q is already registered; ignoring duplicate registration", c.ID)
		return
	}
	log.Println("Registering new checker:", c.ID)
	c.BuildRulesInfo()
	checkerRegistry[c.ID] = c
}

// RegisterExternalizableChecker registers a checker that supports being
// delegated to a remote HTTP endpoint. It appends an "endpoint" AdminOpt
// so the administrator can optionally configure a remote URL.
// When the endpoint is left empty, the checker runs locally as usual.
//
// The duplicate check happens before the AdminOpt append so that a
// rejected second registration does not mutate the in-memory definition
// of the already-registered checker (which a caller might still hold a
// pointer to).
func RegisterExternalizableChecker(c *CheckerDefinition) {
	if _, exists := checkerRegistry[c.ID]; exists {
		log.Printf("Warning: checker %q is already registered; ignoring duplicate registration", c.ID)
		return
	}
	c.Options.AdminOpts = append(c.Options.AdminOpts,
		CheckerOptionDocumentation{
			Id:          "endpoint",
			Type:        "string",
			Label:       "Remote checker endpoint URL",
			Description: "If set, delegate observation collection to this HTTP endpoint instead of running locally.",
			Placeholder: "http://checker-" + c.ID + ":8080",
			NoOverride:  true,
		},
	)
	RegisterChecker(c)
}

// RegisterObservationProvider registers an observation provider globally.
// A second registration under the same key is refused with a warning for
// the same reason as RegisterChecker.
func RegisterObservationProvider(p ObservationProvider) {
	key := p.Key()
	if _, exists := observationProviderRegistry[key]; exists {
		log.Printf("Warning: observation provider %q is already registered; ignoring duplicate registration", key)
		return
	}
	observationProviderRegistry[key] = p
}

// GetCheckers returns all registered checker definitions.
func GetCheckers() map[string]*CheckerDefinition {
	return checkerRegistry
}

// FindChecker returns the checker definition with the given ID, or nil.
func FindChecker(id string) *CheckerDefinition {
	return checkerRegistry[id]
}

// GetObservationProviders returns all registered observation providers.
func GetObservationProviders() map[ObservationKey]ObservationProvider {
	return observationProviderRegistry
}

// FindObservationProvider returns the observation provider for the given key, or nil.
func FindObservationProvider(key ObservationKey) ObservationProvider {
	return observationProviderRegistry[key]
}
