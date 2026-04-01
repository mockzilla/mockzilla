<div style="text-align: center; width:450px;">
    <img src="https://raw.githubusercontent.com/mockzilla/connexions/master/resources/docs/images/gotham.svg">
</div>

## Connexions

[![CI](https://github.com/mockzilla/connexions/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/mockzilla/connexions/actions/workflows/ci.yml?query=branch%3Amaster)
[![Endpoint Badge](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2Fmockzilla%2F4110782af3ec09dd1ebabc3304756f1f%2Fraw%2Fcovbadge.json&labelColor=%23058FF3&color=%2306C53B)](https://github.com/mockzilla/connexions/actions/workflows/ci.yml?query=event%3Apush+branch%3Amaster+workflow%3ACI)
[![codecov](https://codecov.io/gh/mockzilla/connexions/graph/badge.svg?token=XGCEHYUDH0)](https://codecov.io/gh/mockzilla/connexions)
[![GoReportCard](https://goreportcard.com/badge/github.com/mockzilla/connexions)](https://goreportcard.com/report/github.com/mockzilla/connexions)
[![GoDoc](https://godoc.org/github.com/mockzilla/connexions?status.svg)](https://godoc.org/github.com/mockzilla/connexions)
[![Go Reference](https://pkg.go.dev/badge/github.com/mockzilla/connexions.svg)](https://pkg.go.dev/github.com/mockzilla/connexions)
[![License](https://img.shields.io/github/license/mockzilla/connexions)](https://github.com/mockzilla/connexions/blob/master/LICENSE)


**Connexions** is a mock server generator for OpenAPI specifications.<br/>
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

Connexions continuously generates and validates data against **2,200+ real-world OpenAPI specifications** from [mockzilla/specs](https://github.com/mockzilla/specs):

```
Total: 2215 services, 98464 endpoints
✅ Success: 98464  ❌ Fails: 0
```

## Modes

Connexions runs in two modes:

- **[Portable](https://mockzilla.github.io/connexions/usage/portable/)** - point at OpenAPI specs and run. No code generation, no setup.
- **[Codegen](https://mockzilla.github.io/connexions/usage/codegen/)** - generate typed Go handlers with custom logic and middleware.

## Quick Start

### Homebrew

```bash
brew tap mockzilla/tap
brew install connexions
connexions https://petstore3.swagger.io/api/v3/openapi.json
```

### Go

```bash
go run github.com/mockzilla/connexions/v2/cmd/server@latest \
  https://petstore3.swagger.io/api/v3/openapi.json
```

### Templates

- [Portable template](https://github.com/mockzilla/connexions-portable-template) - embed specs into a single binary via `go:embed`
- [Codegen template](https://github.com/mockzilla/connexions-codegen-template) - generate Go handlers with custom logic and middleware

Read full documentation at [mockzilla.github.io/connexions](https://mockzilla.github.io/connexions/).

[OpenAPI Specification](https://editor.swagger.io/?url=https://raw.githubusercontent.com/mockzilla/connexions/master/resources/openapi.yml)

License
===================
Copyright (c) 2023-present

Licensed under the [MIT License](https://github.com/mockzilla/connexions/blob/master/LICENSE)
