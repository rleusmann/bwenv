package main

import (
	"fmt"
	"os"

	"github.com/rleusmann/bwenv/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bwenv:", err)
		os.Exit(1)
	}
}
