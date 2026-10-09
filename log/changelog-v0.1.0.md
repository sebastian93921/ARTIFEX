# ARTEX v0.1.0 — Agent integrations

First standalone ARTEX release. ARTEX versioning starts at v0.1.0; the upstream ARTEX changelog and frontend package version retain their upstream history.

## Added

- Local stdio MCP integration for Claude Code and Codex, plus a native Pi extension.
- Shared authenticated tools to read tasks, coverage and findings, and to create or pause/resume tasks when writes are enabled.
- Input validation, bounded requests, cancellation, credential-redacted errors and explicit failure reporting.
- English/Korean setup instructions and real-backend end-to-end tests.
- Adapter source files included in each platform's release archive; install their Node.js dependencies with `npm ci` in `adapters/agent`.

This release also includes the English/Korean ARTEX edition and GLM provider configuration templates already merged on main. ARTEX is derived from ARTEX at upstream commit `160fe13`; original authorship and AGPL-3.0 terms are preserved.

## Downloads

Platform ZIP archives are built for Linux amd64/arm64, macOS amd64/arm64 and Windows amd64. Each archive includes the embedded web interface, supervisor, skills, configuration example, documentation and agent adapter. `SHA256SUMS` lists the archive checksums. Docker images are built for Linux amd64/arm64 at `ghcr.io/autumn-27/artex`.

## Verification and limitations

- Adapter unit tests, three real-backend E2E scenarios, native Pi extension loading, server regressions and frontend tests passed during adapter delivery. The adapter and frontend GitHub jobs passed on merged main at `d8fadad`.
- Full backend CI retains an inherited failure: `TestGraphOverviewExpandsAssociatedCompanyScope` reports `task scope missing: <nil>`. It also failed before the adapter change; no checks were disabled or weakened.
- Paid model sessions, real target scans and Windows execution were not performed. Model providers and account access must be configured separately.
- Use is subject to the repository's locally isolated research/validation restrictions. This release does not claim production penetration-testing readiness.

See [setup](../README.md), [agent integration](../adapters/agent/README.md) and [verification details](../docs/VERIFICATION.md).
