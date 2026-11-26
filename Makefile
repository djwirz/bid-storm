.PHONY: build storm check test lint

# The newest recording. Another one: make check RUN=runs/<run>
RUN ?= $(lastword $(sort $(wildcard runs/2*)))

# Built rather than `go run`, so the binary carries the git revision the manifest records.
build:
	go build -o bin/bidstorm ./cmd/bidstorm

# Needs a local Holdfast stack already up (README). Flags: make storm ARGS="-rate 100"
storm: build
	./bin/bidstorm $(ARGS)

# Converts a recording to Parquet and checks it. Needs uv.
check:
	@test -n "$(RUN)" || { echo "no recording under runs/; make storm first"; exit 2; }
	uv run --project analysis --locked bidcheck $(RUN)

test:
	go vet ./...
	go test -race ./...
	cd analysis && uv run --locked pytest -q

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	cd analysis && uv run --locked ruff check . && uv run --locked ruff format --check .
