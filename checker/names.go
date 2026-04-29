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

import "strings"

// JoinRelative treats name as relative to origin, as happyDomain encodes
// service-embedded record owners and subdomains. An empty or "@" name
// resolves to the origin itself; an empty origin returns the trimmed name
// unchanged. A name already suffixed by origin is returned as-is so that
// absolute encodings round-trip safely. Trailing dots are stripped.
func JoinRelative(name, origin string) string {
	origin = strings.TrimSuffix(origin, ".")
	name = strings.TrimSuffix(name, ".")
	if origin == "" {
		return name
	}
	if name == "" || name == "@" {
		return origin
	}
	if name == origin || strings.HasSuffix(name, "."+origin) {
		return name
	}
	return name + "." + origin
}
