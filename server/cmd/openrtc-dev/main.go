package main

import (
	"os"

	"github.com/openrtc/openrtc/server/internal/devserver"
)

func main() {
	os.Exit(devserver.Main(os.Args[1:], os.Stdout, os.Stderr))
}
