<div style="text-align: center; width:450px;">
    <img src="https://raw.githubusercontent.com/mockzilla/mockzilla/main/resources/docs/images/gotham.svg">
</div>

## Mockzilla

[![CI](https://github.com/mockzilla/mockzilla/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mockzilla/mockzilla/actions/workflows/ci.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/mockzilla/mockzilla/graph/badge.svg?token=XGCEHYUDH0)](https://codecov.io/gh/mockzilla/mockzilla)
[![GoReportCard](https://goreportcard.com/badge/github.com/mockzilla/mockzilla/v2)](https://goreportcard.com/report/github.com/mockzilla/mockzilla/v2)
[![Go Reference](https://pkg.go.dev/badge/github.com/mockzilla/mockzilla/v2.svg)](https://pkg.go.dev/github.com/mockzilla/mockzilla/v2)
[![License](https://img.shields.io/github/license/mockzilla/mockzilla)](https://github.com/mockzilla/mockzilla/blob/main/LICENSE)


**Mockzilla** is a mock server generator for OpenAPI specifications.<br/>
It allows you to define **multiple APIs** and generate meaningful mock responses automatically.<br/>
You can also define static responses for any arbitrary path.<br/>

## Goals
- provide a simple tool to work with API mocks
- combine multiple APIs into one server
- generate meaningful responses

## Features
- **Multiple APIs** on one server - each spec becomes a service with its own URL prefix
- **Upstream proxy** - forward to real backends with fallback to mocks
- **Latency & error simulation** - test how your app handles delays and failures
- **Custom middleware** - modify requests/responses on the fly
- **Response caching** - cache GET responses for consistency
- **Request validation** - validate against OpenAPI spec

## Real-World Validation

Mockzilla continuously generates and validates data against **2,200+ real-world OpenAPI specifications** from [mockzilla/specs](https://github.com/mockzilla/specs):

```
Total: 2215 services, 98464 endpoints
✅ Success: 98464  ❌ Fails: 0
```

## Modes

Mockazilla runs in two modes:

- **[Portable](https://mockzilla.github.io/mockzilla/usage/portable/)** - point at OpenAPI specs and run. No code generation, no setup.
- **[Codegen](https://mockzilla.github.io/mockzilla/usage/codegen/)** - generate typed Go handlers with custom logic and middleware.

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

[OpenAPI Specification](https://editor.swagger.io/?url=https://raw.githubusercontent.com/mockzilla/mockzilla/main/resources/openapi.yml)

License
===================
Copyright (c) 2023-present

Licensed under the [MIT License](https://github.com/mockzilla/mockzilla/blob/main/LICENSE)
