VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/rannday/bbrs/internal/version.Version=$(VERSION)

.PHONY: build install test fmt vet tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o bbrs ./cmd/bbrs

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/bbrs

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f bbrs