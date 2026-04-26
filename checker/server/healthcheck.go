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
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// healthcheckMode is registered on the default flag set so any consumer that
// calls flag.Parse() before ListenAndServe (the standard pattern in our
// checker mains) gets the behaviour for free. When set, ListenAndServe
// performs a short-lived HTTP probe against /health on the configured listen
// address and exits 0/1 instead of starting the server. This lets the same
// binary act as its own Docker HEALTHCHECK probe for scratch images, where
// no shell, curl or wget is available.
var healthcheckMode = flag.Bool(
	"healthcheck",
	false,
	"probe /health on the server's listen address and exit 0 if healthy, 1 "+
		"otherwise (intended as a Docker HEALTHCHECK for scratch-based images)",
)

// runHealthcheck performs a GET against http://<addr>/health with a short
// timeout. Returns nil on a 2xx response, an error otherwise. A bind address
// like ":8080" or "0.0.0.0:8080" is rewritten to dial the loopback interface
// so the probe targets the local process.
func runHealthcheck(addr string) error {
	host, port, err := net.SplitHostPort(normalizeHealthcheckAddr(addr))
	if err != nil {
		return fmt.Errorf("invalid listen addr %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, port))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
	}
	return nil
}

func normalizeHealthcheckAddr(a string) string {
	if strings.HasPrefix(a, ":") {
		return "127.0.0.1" + a
	}
	if strings.HasPrefix(a, "[::]:") {
		return "[::1]:" + strings.TrimPrefix(a, "[::]:")
	}
	return a
}
