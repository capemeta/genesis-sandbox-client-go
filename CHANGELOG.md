# Changelog

All notable changes to this project will be documented in this file.

## v0.1.1 - 2026-07-26

### Added

- First complete release consumable through Go Modules.
- Public handwritten SDK facade covering Catalog/Resolve, Session, Job, asynchronous Exec/SSE, WorkspaceFS, Artifact, Dependency Build, GUI, and Audit APIs.
- Generated protocol layer under `internal/genapi`, examples, contract tests, and CI/Release workflows.

### Fixed

- Replaces the prematurely created `v0.1.0` tag, which did not contain `go.mod` or SDK source files.

## v0.1.0 - 2026-07-26

This tag was created before the SDK source commit and contains only repository
bootstrap files. It is not a usable Go Module. Use `v0.1.1` or later. The tag is
left unchanged to preserve published Git history.
