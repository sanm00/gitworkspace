VERSION_FILE ?= VERSION
VERSION ?= $(strip $(shell cat $(VERSION_FILE) 2>/dev/null))
GO ?= go

.PHONY: format check test race lint build clean

format:
	gofmt -w .

check:
	test -n "$(VERSION)"
	grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$$' $(VERSION_FILE)
	test -z "$$(gofmt -l .)"
	$(GO) vet ./...
	$(GO) test -race ./...
	$(GO) build -o /dev/null ./cmd/gws

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...

build:
	test -n "$(VERSION)"
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/gws ./cmd/gws

clean:
	$(RM) bin/gws
