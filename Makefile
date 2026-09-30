.PHONY: build test check installers docker
build:
	mkdir -p bin
	CGO_ENABLED=1 go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath -ldflags='-linkmode external -extldflags "-static"' -o bin/redapp ./cmd/redapp
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
