# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state

This repository is an empty scaffold: `go.mod` (module `github.com/gourdian25/grpolicy`, Go 1.26.4), a placeholder `README.md`, an empty `LICENCE`, and `.bark.toml`/`bark.txt`. There is no Go source code, no tests, no build tooling, and no CI configured yet. There are no build/lint/test commands to document until code exists — do not invent a project layout; ask the user what they want to build before scaffolding one.

`.bark.toml` and `bark.txt` are config/output for "bark", a standalone file-header/tree-generation CLI tool — unrelated to grpolicy's build or runtime, do not treat them as part of the module's logic.

## Ecosystem context

The module name and sibling repositories (`grauth`, `graudit`, `grevents`, `grlog`, `grcache`, `gourdiantoken`) indicate this belongs to the "gourdian" ecosystem of Go libraries under github.com/gourdian25, built around auth/authorization concerns:

- `gourdiantoken` — JWT token management (generation, rotation, revocation)
- `grauth` — authentication
- `grpolicy` (this repo) — not yet implemented; name suggests an authorization/policy decision component
- `graudit` — append-only, tamper-evident audit trail (hash-chained)
- `grevents` — lightweight in-process event bus decoupling producers (e.g. `grauth`) from consumers (e.g. `graudit`, `grcache`)
- `grlog` — structured logging with pluggable sinks
- `grcache` — backend-agnostic caching abstraction (one `Cache` interface, multiple backend implementations)

A recurring pattern across these libraries is a narrow interface with swappable backend implementations (e.g. `grcache`'s `Cache` interface, `gourdiantoken`'s `TokenRepository`). If `grpolicy` follows suit, expect a similar interface-plus-backends shape rather than a monolithic implementation — but confirm with the user rather than assuming.
