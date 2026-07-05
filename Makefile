VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PKG := github.com/rannday/bbrs
VERSION_VAR := $(PKG)/internal/version.Version
LDFLAGS := -s -w -X $(VERSION_VAR)=$(VERSION)

.PHONY: build install test fmt vet tidy clean release

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

release:
	go tool go-build-bin \
		-v $(VERSION) \
		--name bbrs \
		--main ./cmd/bbrs \
		--version-var $(VERSION_VAR) \
		--clean \
		--force

clean:
	rm -f bbrs
	rm -rf tmp/release
