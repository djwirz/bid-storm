.PHONY: build storm check site-data site test lint

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

# The published recording as the site's data. Fails if it fails a check.
STORM ?= runs/curated/first-storm
site-data:
	uv run --project analysis --locked bidsite $(STORM) site/public/data/storm.json
	cp runs/curated/sweep/summary.json site/public/data/sweep.json

# Builds the page into site/dist. Needs Node 26 and pnpm.
site: site-data
	cd site && pnpm install --frozen-lockfile && pnpm build

# The site's browser tests are apart: cd site && pnpm test:e2e (after make site).
test: site-data
	go vet ./...
	go test -race ./...
	cd analysis && uv run --locked pytest -q
	cd site && pnpm install --frozen-lockfile && pnpm test

lint: site-data
	test -z "$$(gofmt -l .)"
	go vet ./...
	cd analysis && uv run --locked ruff check . && uv run --locked ruff format --check .
	cd site && pnpm install --frozen-lockfile && pnpm lint && pnpm typecheck
