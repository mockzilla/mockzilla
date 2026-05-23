<p align="center">
  <img
    src="https://raw.githubusercontent.com/mockzilla/mockzilla/main/resources/docs/images/gotham.svg"
    alt="Mockzilla open-source API mock server built with Go"
    width="300"
  />
</p>

## Mockzilla

[![CI](https://github.com/mockzilla/mockzilla/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mockzilla/mockzilla/actions/workflows/ci.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/mockzilla/mockzilla/graph/badge.svg?token=XGCEHYUDH0)](https://codecov.io/gh/mockzilla/mockzilla)
[![GoReportCard](https://goreportcard.com/badge/github.com/mockzilla/mockzilla/v2)](https://goreportcard.com/report/github.com/mockzilla/mockzilla/v2)
[![Go Reference](https://pkg.go.dev/badge/github.com/mockzilla/mockzilla/v2.svg)](https://pkg.go.dev/github.com/mockzilla/mockzilla/v2)
[![mockzilla](https://snapcraft.io/mockzilla/badge.svg)](https://snapcraft.io/mockzilla)
[![License](https://img.shields.io/github/license/mockzilla/mockzilla?cacheSeconds=3600)](https://github.com/mockzilla/mockzilla/blob/main/LICENSE)


**Mockzilla** is an open-source mock server for OpenAPI specifications, validated against 2,200+ real-world OpenAPI specifications across 98,000+ endpoints. Point it at a spec and get a running API mock server in seconds - no code, no configuration, no separate infrastructure. It generates realistic responses from your schema, validates incoming requests against the spec, and can proxy real backends with automatic mock fallback so you can mix real and mocked endpoints in the same server. Use it locally during development, in CI pipelines for integration testing, or embed specs into a portable binary for offline and air-gapped environments.

## Goals
- Generate realistic mock APIs directly from OpenAPI specs, not a custom DSL.
- Combine multiple third-party APIs into a single local mock server.
- Catch contract drift early by validating every request against the spec.

## Features
- **Multiple APIs** on one server - each spec becomes a service with its own URL prefix
- **Upstream proxy** - forward to real backends with fallback to mocks
- **Latency & error simulation** - test how your app handles delays and failures
- **Custom middleware** - modify requests/responses on the fly
- **Response caching** - cache GET responses for consistency
- **Request validation** - validate against OpenAPI spec
- **Static responses** - define custom responses for any path outside your OpenAPI spec
- **Selective overrides** - override specific endpoints with static responses or custom handlers while the rest of the OpenAPI spec stays auto‑generated

## Real-World Validation

Mockzilla continuously generates and validates data against **2,200+ real-world OpenAPI specifications**:

```
Total: 2215 services, 98464 endpoints
✅ Success: 98464  ❌ Fails: 0
```

## Modes

Mockzilla runs in two modes:

- **[Portable](https://mockzilla.github.io/mockzilla/usage/portable/)** - point at OpenAPI specs and run. No code generation, no setup.
- **[Codegen](https://mockzilla.github.io/mockzilla/usage/codegen/)** - generate typed Go handlers with custom logic and middleware.

## Use cases

- **Local development** - mock APIs your backend hasn't built yet
- **CI/CD integration testing** - zero external dependencies in your pipeline
- **PSP and payment API mocking** - Stripe, PayPal, Adyen without sandbox accounts
- **Crypto exchange API mocking** - Binance, Bybit without registered accounts
- **Rate limit protection** - develop against OpenAI, Twilio without burning quota
- **Multi-service testing** - run Stripe + Twilio + SendGrid on one port
- **Offline and air-gapped environments** - embed specs into a portable binary, no internet required
- **Selective endpoint overrides** - keep hundreds of endpoints auto-generated and hand-craft the few that need custom behavior

## Quick Start

### Homebrew

```bash
brew tap mockzilla/tap
brew install mockzilla
mockzilla https://petstore3.swagger.io/api/v3/openapi.json
```

### Go

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/server@latest \
  https://petstore3.swagger.io/api/v3/openapi.json
```

### Templates

- [Portable template](https://github.com/mockzilla/mockzilla-portable-template) - embed specs into a single binary via `go:embed`
- [Codegen template](https://github.com/mockzilla/mockzilla-codegen-template) - generate Go handlers with custom logic and middleware

Read full documentation at [mockzilla.github.io/mockzilla](https://mockzilla.github.io/mockzilla/).

## Mockzilla Cloud

If you need per-PR simulation URLs, team-shared stable endpoints, or zero-config GitHub Action setup without running your own server, [mockzilla.org](https://mockzilla.org) is the hosted version built on the same engine.

## AI and MCP tooling

Mockzilla ships an [MCP server](https://github.com/mockzilla/mockzilla-mcp), so AI tools like Claude Code, Cursor, and Gemini CLI can launch mock APIs directly from any OpenAPI definition. Point the agent at a spec and it spins up a Mockzilla server under the hood, letting you iterate on client code or integration tests without leaving the editor.

## Related
- [mockzilla.org](https://mockzilla.org) - hosted API simulation, per-PR mock URLs, GitHub Actions integration
- [Documentation](https://mockzilla.github.io/mockzilla/) - full usage guide for portable and codegen modes
- [OpenAPI Specification](https://editor.swagger.io/?url=https://raw.githubusercontent.com/mockzilla/mockzilla/main/resources/openapi.yml) - interactive Swagger UI for the Mockzilla API

## License

Copyright © 2023-present

Licensed under the [MIT License](https://github.com/mockzilla/mockzilla/blob/main/LICENSE)