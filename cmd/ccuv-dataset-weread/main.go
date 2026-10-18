// ccuv-dataset-weread implements the one-request ccuv.custom/v2 command.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	_ "time/tzdata"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/dataset"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "ccuv" {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread ccuv")
		os.Exit(2)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read request:", err)
		os.Exit(1)
	}
	response, err := handle(input, func() (dataset.Gateway, error) { return weread.NewHTTPGatewayFromEnvironment() })
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode request:", err)
		os.Exit(1)
	}
	encoded, err := v2.Encode(response)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode response:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "write response:", err)
		os.Exit(1)
	}
}

func handle(input []byte, factory dataset.GatewayFactory) (v2.Response, error) {
	request, err := v2.Decode(input)
	if err != nil {
		if response, ok := v2.InvalidRequestFor(input); ok {
			return response, nil
		}
		return v2.Response{}, err
	}
	return dataset.NewRegistry(factory).Handle(context.Background(), request), nil
}
