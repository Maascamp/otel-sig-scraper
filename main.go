package main

import (
	"os"

	"github.com/gordyrad/otel-sig-tracker/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
