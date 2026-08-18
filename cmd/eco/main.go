package main

import "github.com/toprakkilic/eco/internal/app"

// version is stamped by the build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	app.Main(version)
}
