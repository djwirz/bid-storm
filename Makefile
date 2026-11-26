.PHONY: build storm test

# Built rather than `go run`, so the binary carries the git revision the manifest records.
build:
	go build -o bin/bidstorm ./cmd/bidstorm

# Needs a local Holdfast stack already up (README). Flags: make storm ARGS="-rate 100"
storm: build
	./bin/bidstorm $(ARGS)

test:
	go vet ./...
	go test -race ./...
