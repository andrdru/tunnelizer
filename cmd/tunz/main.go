package main

import (
	"fmt"
	"os"

	"github.com/andrdru/tunnelizer/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tunz:", err)
		os.Exit(1)
	}
}
