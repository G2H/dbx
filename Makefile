# dbx build targets. cgo is required (the DuckDB driver bundles a static
# libduckdb), so builds target the host platform; see README for the
# cross-compile caveat. The release target builds both macOS architectures
# from a Mac, since Apple's clang can target either one.

BINARY := dbx
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST := dist

.PHONY: build test lint clean release

build:
	go build -o $(BINARY) ./cmd/dbx

test:
	go test ./...
	sh scripts/install_test.sh

lint:
	gofmt -l cmd internal | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...

# release builds dist/dbx-darwin-{arm64,amd64}.tar.gz plus checksums.txt.
# Upload them with: gh release create v$(VERSION) dist/*
release:
	rm -rf $(DIST) && mkdir -p $(DIST)
	for arch in arm64 amd64; do \
		CGO_ENABLED=1 GOOS=darwin GOARCH=$$arch CC="clang -arch $$(echo $$arch | sed s/amd64/x86_64/)" \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$$arch/dbx ./cmd/dbx || exit 1; \
		tar -C $(DIST)/$$arch -czf $(DIST)/dbx-darwin-$$arch.tar.gz dbx || exit 1; \
		rm -rf $(DIST)/$$arch; \
	done
	cd $(DIST) && shasum -a 256 *.tar.gz > checksums.txt

clean:
	rm -rf $(BINARY) $(DIST)
