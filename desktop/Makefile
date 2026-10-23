# ============================================================
# desktop/Makefile — Linux and macOS. On Windows use scripts\build-all.ps1.
# The shipped product is the Go server, the Go dashboard program, the Python agent
# (neuro-client, built by scripts/build-client.sh) and the dashboard page.
# ============================================================

GO ?= go
PYTHON ?= python3

all: build

build: server dashboard frontend manager

manager:
	$(PYTHON) scripts/build-manager.py --out dist/manager-source

manager-standalone:
	$(PYTHON) scripts/build-manager.py --standalone

server:
	cd ../headless/apps/neuro-integration && $(GO) build -o dist/neuro-integration .

dashboard:
	cd ../headless/apps/neuro-dashboard && $(GO) build -o dist/neuro-dashboard .

# neuro-client: the agent packaged with PyInstaller, for the PC Neuro controls.
client:
	./scripts/build-client.sh

frontend:
	cd frontend && (test -f package-lock.json && npm ci || npm install) && npm run build

test:
	PYTHONPATH=apps/neuro-manager $(PYTHON) -m unittest discover -s apps/neuro-manager/tests
	cd frontend && npm test
	cd ../headless/apps/neuro-integration && $(GO) vet ./... && $(GO) test -count=1 ./... && $(GO) test -count=1 -tags neurodev ./...
	cd ../headless/apps/neuro-dashboard && $(GO) vet ./... && $(GO) test -count=1 ./...
	cd ../headless/apps/neuro-client-go && $(GO) vet ./... && $(GO) test -count=1 ./...
	cd python && NEURO_HEADLESS=1 $(PYTHON) -m unittest discover -s tests -t .
	cd .. && $(PYTHON) desktop/tools/ci/repo_checks.py
	cd .. && $(PYTHON) -m unittest discover -s desktop/tools/ci -p 'test_*.py'

bundle-prod:
	./scripts/bundle/prod.sh

bundle-dev:
	./scripts/bundle/dev.sh

clean:
	rm -rf ../headless/apps/neuro-integration/dist ../headless/apps/neuro-dashboard/dist frontend/dist dist

.PHONY: all build server dashboard client frontend manager manager-standalone test bundle-prod bundle-dev clean
