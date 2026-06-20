GO ?= go
BIN := prisma

.PHONY: build linux mac windows release install desktop uninstall test clean

# versão a partir da tag git (ex.: v0.1.0, ou v0.1.0-3-gabcdef entre tags)
VERSAO := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VER_LD := -X prisma/internal/update.Versao=$(VERSAO)
LDFLAGS := -trimpath -ldflags "-s -w $(VER_LD)"

build:
	$(GO) build -ldflags "$(VER_LD)" -o bin/$(BIN) ./cmd/prisma

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
	$(MAKE) desktop

# Instala o atalho (.desktop) e o ícone no menu de aplicativos (estilo htop/btop)
desktop:
	mkdir -p $(HOME)/.local/share/applications
	mkdir -p $(HOME)/.local/share/icons/hicolor/scalable/apps
	install -m644 packaging/prisma.svg $(HOME)/.local/share/icons/hicolor/scalable/apps/prisma.svg
	install -m755 packaging/prisma.desktop $(HOME)/.local/share/applications/prisma.desktop
	-gio set $(HOME)/.local/share/applications/prisma.desktop metadata::trusted true 2>/dev/null
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null
	-gtk-update-icon-cache -f -t $(HOME)/.local/share/icons/hicolor 2>/dev/null

uninstall:
	rm -f $(HOME)/.local/bin/$(BIN)
	rm -f $(HOME)/.local/share/applications/prisma.desktop
	rm -f $(HOME)/.local/share/icons/hicolor/scalable/apps/prisma.svg
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null

test:
	$(GO) test ./...

clean:
	rm -rf bin dist
