// Command scenaremo builds narrated slideshow videos from a YAML script.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "scenaremo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: scenaremo <command> [args]")
	}
	return fmt.Errorf("unknown command: %s", args[0])
}
