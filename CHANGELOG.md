# Changelog

All notable changes to this project should be documented in this file.

This project aims to follow semantic versioning once stable releases begin.

## [Unreleased]

### Added

- Baseline Windows CI workflow for Rust, Go, Node, and C++ validation.
- Python parser tests for high-level commands and click syntax.
- Go tests for integration documentation loading.
- `run_script` JSON schema (`action-schema.run_script.json`).
- Production backlog and issue verification tracker (`docs/PRODUCTION_TODO.md`).
- Project vision document (`VISION.md`).
- Optional Neuro Relay process manager and relay-aware runtime routing.
- Docker-based modular test setup (`docker-compose.tests.yml`).
- Optional relay bundling support in PowerShell bundle/build scripts.

### Changed

- Process-handler test build now creates one executable per test source.
- Rust IPC execution path now propagates queue execution/clear failures.
- Action script supports high-level desktop commands.
- Integration docs loading is now non-fatal and supports fallback paths.
- Action script documentation updated to match runtime behavior.
- Go integration migrated to `neuro-integration-sdk` with local compatibility patch.
- High-level Windows action coverage expanded (Explorer, Run, Search, snap-left, snap-right).

### Fixed

- PyO3/Python compatibility blocker for local Python 3.14 environments.
- Mismatch between action docs and `CLICK` parser behavior.
