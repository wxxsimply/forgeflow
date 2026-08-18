.PHONY: fmt fmt-check test race vet staticcheck migration-check build web-install web-check web-e2e verify clean

fmt:
	gofmt -w ./cmd ./internal ./migrations

fmt-check:
	@test -z "$$(gofmt -l ./cmd ./internal ./migrations)" || (echo "The following Go files need gofmt:"; gofmt -l ./cmd ./internal ./migrations; exit 1)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

migration-check:
	go test ./migrations -run TestEmbeddedMigrations

build:
	mkdir -p bin
	go build -trimpath -o bin/forgeflow ./cmd/forgeflow
	go build -trimpath -o bin/forgeflow-worker ./cmd/forgeflow-worker
	go build -trimpath -o bin/forgeflow-api ./cmd/forgeflow-api

web-install:
	cd web && npm ci

web-check:
	cd web && npm run check

web-e2e:
	cd web && npm run test:e2e

verify: fmt-check test vet migration-check build web-check

clean:
	go clean -cache -testcache
