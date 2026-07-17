# dbx build targets. cgo is required (the DuckDB driver bundles a static
# libduckdb), so builds target the host platform; see README for the
# cross-compile caveat.

BINARY := dbx

.PHONY: build test lint clean

build:
	go build -o $(BINARY) ./cmd/dbx

test:
	go test ./...

lint:
	gofmt -l cmd internal | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...

clean:
	rm -f $(BINARY)
