// Command momus is the entry point for the Momus API and FHIR conformance
// testing framework.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is the Momus version. Bumped as part of releases.
const version = "0.0.0"

func main() {
	fs := flag.NewFlagSet("momus", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the Momus version and exit")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `momus - API and FHIR conformance testing framework

Usage:
  momus [flags]

Momus is currently in early scaffolding. This command is the module entry
point; no functional subcommands exist yet.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if *showVersion {
		fmt.Printf("momus %s\n", version)
		return
	}

	fs.Usage()
}
