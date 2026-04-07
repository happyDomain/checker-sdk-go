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

// RegisterChecker registers a checker definition globally.
func RegisterChecker(c *CheckerDefinition) {
	log.Println("Registering new checker:", c.ID)
	c.BuildRulesInfo()
	checkerRegistry[c.ID] = c
}

// RegisterExternalizableChecker registers a checker that supports being
// delegated to a remote HTTP endpoint. It appends an "endpoint" AdminOpt
// so the administrator can optionally configure a remote URL.
// When the endpoint is left empty, the checker runs locally as usual.
func RegisterExternalizableChecker(c *CheckerDefinition) {
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
func RegisterObservationProvider(p ObservationProvider) {
	observationProviderRegistry[p.Key()] = p
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
