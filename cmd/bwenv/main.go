package main

import (
	"os"

	"github.com/rleusmann/bwenv/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
