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

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
