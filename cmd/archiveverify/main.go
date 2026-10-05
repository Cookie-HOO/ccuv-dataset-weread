// ccuv-dataset-weread-verify validates a release archive before distribution.
package main

import (
	"fmt"
	"os"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/packageverify"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread-verify ARCHIVE")
		os.Exit(2)
	}
	if err := packageverify.Verify(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "invalid archive:", err)
		os.Exit(1)
	}
	digest, err := packageverify.SHA256(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(digest)
}
