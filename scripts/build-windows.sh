#!/bin/sh
# Prepares the cross-compile toolchain (if needed) and builds eco.exe.
# Everything installs user-locally; no root required.
set -e

MINGW="$HOME/.local/mingw"
CC_BIN="$MINGW/usr/bin/x86_64-w64-mingw32-gcc"

# 1. Go toolchain
if ! command -v go >/dev/null 2>&1; then
    if [ -x "$HOME/go/bin/go" ]; then
        PATH="$HOME/go/bin:$PATH"
    else
        echo "Go is not installed. Install it from https://go.dev/dl/ first." >&2
        exit 1
    fi
fi

# 2. MinGW cross-compiler, extracted from Debian packages into ~/.local/mingw
if [ ! -x "$CC_BIN" ]; then
    echo "MinGW not found, downloading it into $MINGW ..."
    command -v apt-get >/dev/null 2>&1 || { echo "apt-get unavailable; install mingw-w64 manually." >&2; exit 1; }
    tmp=$(mktemp -d)
    (
        cd "$tmp"
        apt-get download \
            gcc-mingw-w64-x86-64-win32 gcc-mingw-w64-x86-64-win32-runtime \
            binutils-mingw-w64-x86-64 mingw-w64-common mingw-w64-x86-64-dev
        mkdir -p root
        for d in *.deb; do dpkg -x "$d" root; done
    )
    mkdir -p "$(dirname "$MINGW")"
    rm -rf "$MINGW"
    mv "$tmp/root" "$MINGW"
    rm -rf "$tmp"
    ln -sf x86_64-w64-mingw32-gcc-win32 "$CC_BIN"
fi
echo "Using $($CC_BIN --version | head -1)"

# 3. Cross-compile
cd "$(dirname "$0")/.."
PATH="$MINGW/usr/bin:$PATH" \
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
go build -ldflags -H=windowsgui -o dist/eco.exe ./cmd/eco

echo
echo "Built $(pwd)/dist/eco.exe"
ls -la dist/eco.exe
