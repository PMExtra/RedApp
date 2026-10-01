VERSION ?= $(shell cat VERSION)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

.PHONY: build test check installers docker frontend frontend-test
build: frontend
	mkdir -p bin
	CGO_ENABLED=1 go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath -ldflags='-linkmode external -extldflags "-static" -X main.version=$(VERSION) -X main.revision=$(REVISION)' -o bin/redapp ./cmd/redapp
test:
	go test -race ./... -count=1 -timeout=120s
	python3 scripts/test-installers.py
	python3 scripts/test-update-installers.py
check:
	test -z "$$(gofmt -l cmd internal installers/codex/assets.go)"
	go vet ./...
installers:
	python3 scripts/update-installers.py --source installers/codex/upstream
docker:
	docker build -t redapp:local .

frontend/node_modules/.package-lock.json: frontend/package.json frontend/package-lock.json
	cd frontend && npm ci --no-audit --no-fund
frontend: frontend/node_modules/.package-lock.json
	cd frontend && npm run build
frontend-test: frontend/node_modules/.package-lock.json
	cd frontend && npm run typecheck && npm test
