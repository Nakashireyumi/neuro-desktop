# ============================================================
# desktop/Makefile
# ============================================================

all: native frontend rust go

native:
	cd native/c_cpp && cmake -S . -B build && cmake --build build

go:
	cd native/go-neuro-integration && go build -o go-neuro-integration main.go

frontend:
	cd frontend && npm install && npm run build

rust:
	cd apps/neuro-desktop && cargo build --release

bundle-dev: go rust
	powershell.exe .\scripts\bundle\dev.ps1

bundle-prod: go rust frontend
	powershell.exe .\scripts\bundle\prod.ps1

clean:
	rm -rf native/**/build frontend/dist apps/neuro-desktop/target dist

run: bundle-dev
	cd apps/neuro-desktop/target/release && ./neuro-desktop

.PHONY: all native go frontend rust bundle-dev bundle-prod clean run
