GO ?= go
BIN := prisma

.PHONY: build linux mac windows release install test clean

LDFLAGS := -trimpath -ldflags "-s -w"

build:
	$(GO) build -o bin/$(BIN) ./cmd/prisma

# Binário Linux estático (sem dependências de sistema)
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BIN)-linux-amd64 ./cmd/prisma

mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/$(BIN)-mac-arm64 ./cmd/prisma
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BIN)-mac-intel ./cmd/prisma

windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BIN)-windows-amd64.exe ./cmd/prisma
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/$(BIN)-windows-arm64.exe ./cmd/prisma

release: linux mac windows

install: linux
	mkdir -p $(HOME)/.local/bin
	cp dist/$(BIN)-linux-amd64 $(HOME)/.local/bin/$(BIN)

test:
	$(GO) test ./...

clean:
	rm -rf bin dist
