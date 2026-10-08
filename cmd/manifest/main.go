// ccuv-dataset-weread-manifest writes a generated Dataset manifest.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/dataset"
)

func main() {
	targetOS := flag.String("os", "darwin", "target operating system")
	targetArch := flag.String("arch", "arm64", "target architecture")
	output := flag.String("out", "", "output file; stdout when omitted")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread-manifest [--os OS] [--arch ARCH] [--out FILE]")
		os.Exit(2)
	}
	var writer io.Writer = os.Stdout
	var file *os.File
	var err error
	if *output != "" {
		file, err = os.Create(*output)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create manifest:", err)
			os.Exit(1)
		}
		defer file.Close()
		writer = file
	}
	if err := dataset.WriteManifest(writer, *targetOS, *targetArch); err != nil {
		fmt.Fprintln(os.Stderr, "write manifest:", err)
		os.Exit(1)
	}
}
