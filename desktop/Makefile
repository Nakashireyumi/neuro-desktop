# ============================================================
# desktop/Makefile — works on Linux/macOS; uses PowerShell on Windows if present
# ============================================================

UNAME_S := $(shell uname -s 2>/dev/null || echo Windows)
BIN_EXT :=
ifeq ($(OS),Windows_NT)
  BIN_EXT := .exe
  BUNDLE_DEV := powershell.exe -File scripts/bundle/dev.ps1
  BUNDLE_PROD := powershell.exe -File scripts/bundle/prod.ps1
else
  BUNDLE_DEV := ./scripts/bundle/dev.sh
  BUNDLE_PROD := ./scripts/bundle/prod.sh
endif

all: bundle-prod

native:
	cd apps/process-handler && cmake -S . -B build && cmake --build build

go:
	cd apps/neuro-integration && go build -o dist/neuro-integration$(BIN_EXT) .

frontend:
	cd frontend && npm install && npm run build

rust:
	cd apps/neuro-desktop && cargo build --release

bundle-dev: rust
	$(BUNDLE_DEV)

bundle-prod: rust
	$(BUNDLE_PROD)

clean:
	rm -rf apps/process-handler/build apps/neuro-integration/dist frontend/dist apps/neuro-desktop/target dist

run: bundle-dev

.PHONY: all native go frontend rust bundle-dev bundle-prod clean run
