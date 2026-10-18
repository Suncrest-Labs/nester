// Command config-doc generates or verifies docs/api-config.md from the
// canonical environment variable specifications in internal/config (#1562).
//
// Usage:
//
//	go run ./cmd/config-doc > ../../docs/api-config.md
//	go run ./cmd/config-doc -check ../../docs/api-config.md
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
)

func main() {
	checkPath := flag.String("check", "", "path to docs/api-config.md to verify against generated output")
	writePath := flag.String("write", "", "optional file path to write generated markdown to")
	flag.Parse()

	generated := config.GenerateMarkdownReference()

	if *writePath != "" {
		if err := os.WriteFile(*writePath, []byte(generated), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "config-doc: writing %s: %v\n", *writePath, err)
			os.Exit(1)
		}
		return
	}

	if *checkPath == "" {
		if _, err := os.Stdout.WriteString(generated); err != nil {
			fmt.Fprintf(os.Stderr, "config-doc: writing stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}

	committedBytes, err := os.ReadFile(*checkPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config-doc: reading %s: %v\n", *checkPath, err)
		os.Exit(1)
	}

	committed := strings.ReplaceAll(string(committedBytes), "\r\n", "\n")
	if committed != generated {
		fmt.Fprintf(os.Stderr, "config-doc: %s is out of date with internal/config.\n", *checkPath)
		fmt.Fprintf(os.Stderr, "Run: cd apps/api && go run ./cmd/config-doc -write ../../docs/api-config.md\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "config-doc: %s is up to date\n", *checkPath)
}
