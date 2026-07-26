# Changelog

All notable changes to this project will be documented in this file.

## v0.1.0 - 2026-07-26

### Added

- Initial standalone `genesis-sandbox-client-go` module published from the extracted Go SDK.
- Public handwritten SDK facade covering client APIs, higher-level session helpers, SSE handling, and examples.
- Generated protocol layer under `internal/genapi` backed by an OpenAPI snapshot.

### Changed

- Standardized public semantics for `profile`, `language`, `hints`, and `resolution_id`.
- Preferred `WithHints(...)` for portable environment selection while preserving `WithProfile(...)` for explicit deployment-local binding.
- Documented local source integration through `go.work` for service + SDK joint debugging.

### CI / Release

- Added `ci` workflow running `go test ./...`.
- Added tag-driven `release` workflow that verifies, packages, and publishes GitHub Releases.
