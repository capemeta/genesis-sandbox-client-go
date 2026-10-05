# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

### Added

- `Client.LookupSession` (`GET /v1/sessions:lookup`), `Client.ResumeSession`
  (`POST /v1/sessions/{id}:resume`), `Client.GetExecByOperation`
  (`GET /v1/sessions/{id}/execs:lookup`), and the `Sandbox.Resume` facade method.
- `ExecSessionResult` now exposes `exec_id`, `status`, `error_code`, and
  `effective_environment` (parsed end to end; integration-verified).

### Changed

- Protocol snapshot `api/openapi.yaml` regenerated from the service contract
  (2026-10-01): required fields became non-pointer, exec/session/job statuses
  became typed enums, `SessionContext` is cwd-only, and the shared-storage
  schemas are inlined for codegen. Adapter updated accordingly.
- `CreateSessionRequest.Env` removed and `Sandbox.SetEnv` is now a client-side
  default merged into every exec (the server-side session context no longer
  accepts env; sending it is rejected as an unknown field).
- `ExecResult` from the session path now propagates `ErrorCode` and
  `EffectiveEnvironment` (cancelled sync execs no longer look like success).
- `go.mod` bumped to `go 1.24.0` (oapi-codegen v2.8.0 toolchain requirement).

### Fixed

- `interrupted` is now treated as a terminal status by `WaitJob`, `WaitJobWithOptions`,
  and `ExecHandle.Wait` — previously an interrupted job/exec was polled forever.
  This matches the Python SDK and the service contract.

### Changed

- `NewClient` now enforces the Platform endpoint constraints shared with the
  sibling SDKs: `BaseURL` must not embed credentials, a query, a fragment, or a
  path prefix, and plain `http` is only allowed for loopback hosts
  (`localhost`/`127.0.0.1`/`::1`).
- `Token` is validated up front: surrounding whitespace and CR/LF are rejected
  to prevent Authorization header injection.
- The default HTTP client refuses to follow redirects (3xx surfaces as a
  structured `APIError`), so Bearer credentials are never forwarded beyond the
  configured `BaseURL`. Callers supplying their own `Config.HTTPClient` are
  responsible for the same discipline.
- JSON response decoding is bounded by a 16 MiB budget so a misbehaving
  server cannot exhaust client memory.

### Removed

- Redundant Go 1.22 loop-variable copies (`i, job := i, job`, `valueCopy`).

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
