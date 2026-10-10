VERSION ?= $(shell cat VERSION)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

NODE_MODULES := frontend/node_modules/.package-lock.json
# Explicit roots keep Go tooling out of frontend/node_modules (some npm packages ship Go files).
GO_PACKAGES := ./cmd/... ./installers/... ./internal/... ./presets/...
# runtime-test and network-test exercise the existing binary on purpose (CI builds
# it in the pinned native container); they never rebuild it.
REQUIRE_BINARY := @test -x bin/redapp || { echo 'bin/redapp is missing: run `make binary` or `make build` first' >&2; exit 1; }

.PHONY: build binary check test docs-check toolchain-check frontend frontend-check frontend-test \
	runtime-test e2e network-test installers installer-inventory docker

build: frontend
	@$(MAKE) --no-print-directory binary
binary:
	sh scripts/build-binary.sh bin/redapp "$(VERSION)" "$(REVISION)"

check: docs-check toolchain-check
	test -z "$$(gofmt -l cmd internal installers presets)"
	go vet $(GO_PACKAGES)
docs-check:
	python3 scripts/check-docs.py
toolchain-check:
	python3 scripts/check-toolchain.py

test: installer-inventory
	python3 scripts/test-ci-release.py
	python3 scripts/test-check-docs.py
	python3 scripts/test-check-toolchain.py
	go test -race $(GO_PACKAGES) -count=1 -timeout=180s
	python3 scripts/test-installers.py --platform shell
	python3 scripts/test-update-installers.py
	python3 scripts/test-installer-maintenance.py

$(NODE_MODULES): frontend/package.json frontend/package-lock.json
	cd frontend && npm ci --no-audit --no-fund
frontend: $(NODE_MODULES)
	cd frontend && npm run build
# Rebuild and require the committed bundle byte for byte; untracked files fail too.
frontend-check: frontend
	git diff --exit-code -- internal/httpserver/web
	test -z "$$(git status --porcelain -- internal/httpserver/web)"
frontend-test: $(NODE_MODULES)
	cd frontend && npm run codegen:check && npm run lint && npm run format:check && npm run typecheck && npm test

# Real processes on loopback ports; the instructions test needs frontend/node_modules (Happy DOM).
runtime-test: $(NODE_MODULES)
	$(REQUIRE_BINARY)
	python3 scripts/test-data-cli.py
	python3 scripts/test-http-cli.py
	node scripts/test-instructions-document.mjs
	python3 scripts/test-retention-cli.py
	python3 scripts/test-prewarm-cli.py
	python3 scripts/test-taxonomy-cli.py
	python3 scripts/test-configuration-exchange-cli.py

# Playwright against the existing binary with a fresh data directory; needs the
# Chromium browser: cd frontend && npx playwright install --with-deps chromium
e2e: $(NODE_MODULES)
	$(REQUIRE_BINARY)
	python3 scripts/test-e2e.py

# Manual, needs Internet access: official signed Claude manifest and one real binary (>200 MB).
network-test:
	$(REQUIRE_BINARY)
	python3 scripts/test-prewarm-claude-cli.py

installer-inventory:
	mkdir -p .generated
	go run ./cmd/preset-inventory > .generated/installer-inventory.json.tmp
	mv .generated/installer-inventory.json.tmp .generated/installer-inventory.json
installers: installer-inventory
	python3 scripts/update-installers.py --application openai/codex --source installers/openai/codex/upstream
	python3 scripts/update-installers.py --application anthropic/claude-code --source installers/anthropic/claude-code/upstream

docker:
	docker build -t redapp:local --build-arg VERSION="$(VERSION)" --build-arg REVISION="$(REVISION)" .
