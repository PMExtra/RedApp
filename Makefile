VERSION ?= $(shell cat VERSION)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

.PHONY: build binary test check installers docker frontend frontend-test
build: frontend
	sh scripts/build-binary.sh bin/redapp "$(VERSION)" "$(REVISION)"
binary:
	sh scripts/build-binary.sh bin/redapp "$(VERSION)" "$(REVISION)"
test:
	python3 scripts/test-ci-release.py
	go test -race ./... -count=1 -timeout=180s
	python3 scripts/test-installers.py --platform shell
	python3 scripts/test-update-installers.py
	python3 scripts/test-installer-maintenance.py
check:
	test -z "$$(gofmt -l cmd internal installers)"
	go vet ./...
installers:
	python3 scripts/update-installers.py --application openai/codex --source installers/openai/codex/upstream
	python3 scripts/update-installers.py --application anthropic/claude-code --source installers/anthropic/claude-code/upstream
docker:
	docker build -t redapp:local .

frontend/node_modules/.package-lock.json: frontend/package.json frontend/package-lock.json
	cd frontend && npm ci --no-audit --no-fund
frontend: frontend/node_modules/.package-lock.json
	cd frontend && npm run build
frontend-test: frontend/node_modules/.package-lock.json
	cd frontend && npm run typecheck && npm test
