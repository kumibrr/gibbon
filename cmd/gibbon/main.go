package main

import (
	"os"

	"github.com/kumibrr/gibbon/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
