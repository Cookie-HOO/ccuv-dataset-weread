// ccuv-dataset-weread-package builds a deterministic release archive.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/packagebuild"
)

func main() {
	output := flag.String("out", "", "archive output path")
	manifest := flag.String("manifest", "", "manifest path")
	executable := flag.String("executable", "", "compiled executable path")
	entrypoint := flag.String("entrypoint", "bin/ccuv-dataset-weread", "archive executable path")
	flag.Parse()
	if *output == "" || *manifest == "" || *executable == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread-package --out ARCHIVE --manifest MANIFEST --executable BINARY [--entrypoint PATH]")
		os.Exit(2)
	}
	if err := packagebuild.Write(*output, *manifest, *executable, *entrypoint); err != nil {
		fmt.Fprintln(os.Stderr, "package archive:", err)
		os.Exit(1)
	}
}
