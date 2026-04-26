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

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHealthcheck_OK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := runHealthcheck(addr); err != nil {
		t.Fatalf("runHealthcheck(%s) returned error: %v", addr, err)
	}
}

func TestRunHealthcheck_NonOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := runHealthcheck(addr); err == nil {
		t.Fatalf("runHealthcheck against 503 returned nil; want error")
	}
}

func TestRunHealthcheck_Unreachable(t *testing.T) {
	// Reserved-for-documentation port on loopback that nothing should bind.
	if err := runHealthcheck("127.0.0.1:1"); err == nil {
		t.Fatalf("runHealthcheck against unreachable port returned nil; want error")
	}
}

func TestNormalizeHealthcheckAddr(t *testing.T) {
	cases := map[string]string{
		":8080":          "127.0.0.1:8080",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"0.0.0.0:8080":   "0.0.0.0:8080",
		"[::1]:8080":     "[::1]:8080",
		"[::]:8080":      "[::1]:8080",
	}
	for in, want := range cases {
		if got := normalizeHealthcheckAddr(in); got != want {
			t.Errorf("normalizeHealthcheckAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
