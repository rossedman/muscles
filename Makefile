.PHONY: build test check fmt vet lint cover docs demo release-snapshot

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

check: fmt vet test

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
