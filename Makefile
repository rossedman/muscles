.PHONY: build test check fmt vet lint cover cmds doccheck vuln bench bench-smoke fuzz docs demo release-snapshot

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null)
LDFLAGS := -X github.com/rossedman/muscles/internal/version.Version=$(VERSION) -X github.com/rossedman/muscles/internal/version.Commit=$(COMMIT)

build:
	go build -ldflags "$(LDFLAGS)" -o muscles ./cmd/muscles

test:
	go test ./...

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

check: fmt vet cmds doccheck test

# One command per file in internal/cli.
cmds:
	./tools/check-cmds.sh

# Every function, method and package has a doc comment.
doccheck:
	go run ./tools/doccheck ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# Benchmarks, saved for benchstat: make bench; then compare bench.txt
# against a later run with benchstat old.txt bench.txt.
bench:
	go test -run='^$$' -bench=. -benchmem -count=6 ./... | tee bench.txt

# Every benchmark once, to prove they still run.
bench-smoke:
	go test -run='^$$' -bench=. -benchtime=1x ./...

# Each fuzz target for FUZZTIME (go test fuzzes one target at a time).
FUZZTIME ?= 10s
fuzz:
	@set -e; for pkg in $$(go list ./...); do \
	  for t in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz' || true); do \
	    echo "fuzz $$pkg $$t"; go test -run='^$$' -fuzz="^$$t$$" -fuzztime=$(FUZZTIME) $$pkg; \
	  done; \
	done

lint:
	golangci-lint run ./...

# Total coverage must stay at 90% or more (CI fails below it).
cover:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -func=coverage.out | awk '/^total:/ { sub("%", "", $$3); if ($$3 + 0 < 90) { print "coverage below 90%"; exit 1 } }'

# Build the docs site outside the repository (pip install -r docs/requirements.txt).
docs:
	mkdocs build --strict -d /tmp/muscles-site

# Write the demo images used for the docs screenshots into /tmp/muscles-demo.
demo:
	mkdir -p /tmp/muscles-demo
	MUSCLES_DEMO_DIR=/tmp/muscles-demo go test ./internal/testimg -run TestWriteDemo -count=1

# Build every release archive into dist/ without publishing or signing (needs goreleaser).
release-snapshot:
	goreleaser release --snapshot --clean --skip=sign
