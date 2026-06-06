.PHONY: generate test vet build run up down tidy

# Regenerate Go code from .templ files. Run this before `build`/`run` locally.
generate:
	go tool templ generate

test: generate
	go test ./...

vet: generate
	go vet ./...

build: generate
	go build -o bin/linker ./cmd/linker

# Run locally (expects a reachable DATABASE_URL and a populated .env).
run: generate
	go run ./cmd/linker

# Full stack: Postgres + app.
up:
	docker compose up --build

down:
	docker compose down

tidy:
	go mod tidy
