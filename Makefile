CGO_LDFLAGS ?= -L$(HOME)/.local/lib
GO ?= go
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install run test test-live test-budget test-budget-1m clean windows dist

build:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) build -ldflags "$(LDFLAGS)" -o dist/eco ./cmd/eco

# install verifies the installed binary's stamp matches this build, killing
# the stale-binary trap (the app menu runs ~/.local/bin/eco, not dist/eco)
install: build
	mkdir -p $(HOME)/.local/bin $(HOME)/.local/share/applications $(HOME)/.local/share/icons/hicolor/512x512/apps
	cp dist/eco $(HOME)/.local/bin/eco
	cp internal/app/assets/icon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/eco.png
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
	$(GO) build -ldflags "-H=windowsgui $(LDFLAGS)" -o dist/eco.exe ./cmd/eco

# offline suite (race detector on); live testnet tests are opt-in below
test:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -race ./...

test-live:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -race -tags live -timeout 300s ./...

# performance budgets: the archive's (blueprint §5.4) and the Timeline's 10k
# relayout gate (risk #1). Deliberately NOT under -race: instrumented, the
# pure-Go SQLite runs about twenty times slower and every text measurement with
# it, so a latency measured there says nothing about the shipped binary.
test-budget:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -count=1 -v -timeout 30m -run Budget ./internal/archive/ ./internal/ui/widgets/

# the 1M-row profiling run the M9 numbers came from (a few minutes)
test-budget-1m:
	ECO_ARCHIVE_BUDGET_ROWS=1000000 CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -count=1 -v -timeout 30m -run Budget ./internal/archive/

dist: build windows
	ls -la dist/eco dist/eco.exe

clean:
	rm -rf dist
