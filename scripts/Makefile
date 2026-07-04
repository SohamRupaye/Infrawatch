MODULE := github.com/infrawatch/infrawatch
ENGINE_BIN := bin/engine
API_BIN    := bin/api

.PHONY: all build build-engine build-api run-engine run-api \
        test lint tidy vet fmt \
        docker-up docker-down docker-up-dev docker-down-dev \
        clean

# ─── Build ──────────────────────────────────────────────────────────────────

all: build

build: build-engine build-api

build-engine:
	@mkdir -p bin
	go build -ldflags="-s -w" -o $(ENGINE_BIN) ./apps/engine/cmd/...
	@echo "✓ engine binary → $(ENGINE_BIN)"

build-api:
	@mkdir -p bin
	go build -ldflags="-s -w" -o $(API_BIN) ./apps/api/cmd/...
	@echo "✓ api binary → $(API_BIN)"

# ─── Run (local, no Docker) ─────────────────────────────────────────────────

run-engine:
	INFRAWATCH_CONFIG=infrawatch.yaml go run ./apps/engine/cmd/...

run-api:
	INFRAWATCH_CONFIG=infrawatch.yaml GIN_MODE=debug go run ./apps/api/cmd/...

# ─── Code quality ───────────────────────────────────────────────────────────

test:
	go test ./... -race -count=1

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	@which golangci-lint > /dev/null || (echo "install golangci-lint first: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

tidy:
	go mod tidy

# ─── Docker ─────────────────────────────────────────────────────────────────

docker-up:
	docker compose -f docker/docker-compose.yml up --build -d
	@echo "✓ stack running — API at http://localhost:8080"

docker-down:
	docker compose -f docker/docker-compose.yml down

docker-up-dev:
	docker compose -f docker/docker-compose.dev.yml up --build

docker-down-dev:
	docker compose -f docker/docker-compose.dev.yml down

# ─── Misc ────────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/
