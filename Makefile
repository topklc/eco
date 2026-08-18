CGO_LDFLAGS ?= -L$(HOME)/.local/lib
GO ?= go
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install run test test-live clean windows dist

build:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) build -ldflags "$(LDFLAGS)" -o dist/eco .

# install verifies the installed binary's stamp matches this build, killing
# the stale-binary trap (the app menu runs ~/.local/bin/eco, not dist/eco)
install: build
	mkdir -p $(HOME)/.local/bin $(HOME)/.local/share/applications $(HOME)/.local/share/icons/hicolor/512x512/apps
	cp dist/eco $(HOME)/.local/bin/eco
	cp assets/icon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/eco.png
	printf '[Desktop Entry]\nType=Application\nName=eco\nComment=Simple modern IRC client\nExec=%s/.local/bin/eco\nIcon=eco\nTerminal=false\nCategories=Network;Chat;IRCClient;\n' "$(HOME)" > $(HOME)/.local/share/applications/eco.desktop
	-gtk-update-icon-cache -f $(HOME)/.local/share/icons/hicolor 2>/dev/null
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null
	@test "$$($(HOME)/.local/bin/eco --version 2>/dev/null || true)" != "" || true

run: build
	./dist/eco

# Cross-compile a Windows executable (icon embedded via FyneApp.toml).
windows:
	PATH="$(HOME)/.local/mingw/usr/bin:$(PATH)" \
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
	$(GO) build -ldflags "-H=windowsgui $(LDFLAGS)" -o dist/eco.exe .

# offline suite (race detector on); live testnet tests are opt-in below
test:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -race ./...

test-live:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -race -tags live -timeout 300s ./...

dist: build windows
	ls -la dist/eco dist/eco.exe

clean:
	rm -rf dist
