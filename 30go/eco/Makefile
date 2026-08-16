CGO_LDFLAGS ?= -L$(HOME)/.local/lib
GO ?= go

.PHONY: build install run test clean windows dist

build:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) build -o dist/eco .

install: build
	mkdir -p $(HOME)/.local/bin $(HOME)/.local/share/applications $(HOME)/.local/share/icons/hicolor/512x512/apps
	cp dist/eco $(HOME)/.local/bin/eco
	cp assets/icon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/eco.png
	printf '[Desktop Entry]\nType=Application\nName=eco\nComment=Simple modern IRC client\nExec=%s/.local/bin/eco\nIcon=eco\nTerminal=false\nCategories=Network;Chat;IRCClient;\n' "$(HOME)" > $(HOME)/.local/share/applications/eco.desktop
	-gtk-update-icon-cache -f $(HOME)/.local/share/icons/hicolor 2>/dev/null
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null

run: build
	./dist/eco

# Cross-compile a Windows executable (icon embedded via FyneApp.toml).
# Uses the local MinGW toolchain in ~/.local/mingw.
windows:
	PATH="$(HOME)/.local/mingw/usr/bin:$(PATH)" \
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
	$(GO) build -ldflags -H=windowsgui -o dist/eco.exe .

test:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test .

# Release binaries: bare executables, docs are embedded in-app.
dist: build windows
	ls -la dist/eco dist/eco.exe

clean:
	rm -rf dist
