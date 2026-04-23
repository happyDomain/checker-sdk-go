# checker-sdk-go

Public Go SDK for writing [happyDomain](https://happydomain.org) checker plugins.

This module provides the stable types, helpers, and HTTP server scaffolding
that all checker plugins need, independent of the happyDomain server itself.

## Why a separate module?

The happyDomain server is licensed under AGPL-3.0 (with a commercial option).
If checkers had to import the server's internal packages, every checker, even
trivial ones, would inherit those licensing constraints.

This SDK is released under the **Apache License 2.0**, so:

- You can write a checker plugin under any license you want (MIT, Apache,
  proprietary, AGPL, whatever fits your needs).
- A plugin built against this SDK is *not* a derivative work of happyDomain.
- happyDomain itself depends on this SDK (as an Apache-licensed dependency,
  which is compatible with AGPL).

## Installation

```bash
go get git.happydns.org/checker-sdk-go/checker
```

## Getting started

See [checker-dummy](https://git.happydns.org/checker-dummy) for a
fully working, documented template.

## Extending the server

`checker.Server` exposes the standard SDK routes (`/health`, `/collect`,
and, depending on the provider's optional interfaces, `/definition`,
`/evaluate`, `/report`). Plugins that need to serve auxiliary endpoints
(debug pages, webhooks, custom UI assets, …) can register them on the
same mux:

```go
srv := checker.NewServer(provider)

srv.HandleFunc("GET /debug/state", func(w http.ResponseWriter, r *http.Request) {
    // …
})

// Opt a custom route into the in-flight / load-average signal
// reported on /health:
srv.Handle("POST /webhook", srv.TrackWork(myWebhookHandler))

log.Fatal(srv.ListenAndServe(":8080"))
```

Patterns that collide with built-in routes panic at registration —
pick non-overlapping paths. Custom handlers are not wrapped by the
load-tracking middleware unless you opt in via `TrackWork`.

## Standalone human UI (`/check`)

Providers that implement `CheckerInteractive` get a built-in human-facing
web form on `/check`, usable outside of happyDomain:

```go
type CheckerInteractive interface {
    RenderForm() []CheckerOptionField
    ParseForm(r *http.Request) (CheckerOptions, error)
}
```

- `GET /check` renders a form derived from `RenderForm()`.
- `POST /check` calls `ParseForm` to obtain `CheckerOptions`, runs the
  standard `Collect` → `Evaluate` → `GetHTMLReport` / `ExtractMetrics`
  pipeline, and returns a consolidated HTML page.

`ParseForm` is where the checker replaces what happyDomain would normally
auto-fill (zone records, service payload, …) — typically by issuing its
own DNS queries from the human-supplied inputs.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
