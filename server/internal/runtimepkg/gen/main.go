// Command gen packs the runtime packages' files (runtimepkg.Packages) from integrations/ into dist/, which personad
// embeds: `go generate ./internal/runtimepkg`, run from server/ before building (the Dockerfile and the deploy and
// publish scripts do).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nwverse/persona/server/internal/runtimepkg"
)

func main() {
	src := flag.String("src", filepath.Join("..", "..", "..", "integrations"), "the integrations directory")
	out := flag.String("out", "dist", "where the packages go (its README.md stays)")
	flag.Parse()
	pkgs, err := runtimepkg.Generate(*src, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtimepkg gen:", err)
		os.Exit(1)
	}
	names := make([]string, 0, len(pkgs))
	for name := range pkgs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("runtimepkg: %s %s (%d files)\n", name, runtimepkg.Hash(pkgs[name])[:12], len(pkgs[name]))
	}
}
