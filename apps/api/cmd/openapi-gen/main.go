// Command openapi-gen regenerates the committed OpenAPI 3.1 specification
// from the Go source (nester#1058), so the spec is a build artifact rather
// than a hand-maintained document that drifts from the implementation.
//
// Usage:
//
//	go run ./cmd/openapi-gen > openapi.yaml
//	go run ./cmd/openapi-gen -check openapi.yaml
//
// -check compares the freshly generated spec against the given file and
// exits non-zero (with a diff on stderr) if they differ, without writing
// anything — this is what CI runs to catch a stale committed spec.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/suncrestlabs/nester/apps/api/internal/openapi"
)

func main() {
	checkPath := flag.String("check", "", "path to an existing spec file to diff against, instead of printing to stdout")
	flag.Parse()

	reflector, err := openapi.NewReflector()
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-gen: building spec: %v\n", err)
		os.Exit(1)
	}

	generated, err := reflector.Spec.MarshalYAML()
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-gen: marshaling spec: %v\n", err)
		os.Exit(1)
	}

	if *checkPath == "" {
		if _, err := os.Stdout.Write(generated); err != nil {
			fmt.Fprintf(os.Stderr, "openapi-gen: writing to stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}

	committed, err := os.ReadFile(*checkPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-gen: reading %s: %v\n", *checkPath, err)
		os.Exit(1)
	}

	if string(committed) != string(generated) {
		fmt.Fprintf(os.Stderr, "openapi-gen: %s is stale — the committed spec no longer matches what the Go source generates.\n", *checkPath)
		fmt.Fprintf(os.Stderr, "Run: go run ./cmd/openapi-gen > %s\n\n", *checkPath)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "openapi-gen: %s is up to date\n", *checkPath)
}
