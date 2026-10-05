// ccuv-dataset-weread implements the one-request ccuv custom command.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
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
	response, err := handle(input, func() (weread.Gateway, error) {
		return weread.NewHTTPGatewayFromEnvironment()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	write(response)
}

func handle(input []byte, gatewayFactory func() (weread.Gateway, error)) (any, error) {
	request, err := protocol.DecodeRequestBytes(input)
	if err != nil {
		if response, ok := protocol.InvalidRequestFor(input); ok {
			return response, nil
		}
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if err := request.Validate(); err != nil {
		return protocol.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。"), nil
	}
	if request.RequestKind == "probe" {
		return protocol.NewProbeResponse(request), nil
	}
	if response := weread.SelectionError(request); response != nil {
		return *response, nil
	}
	gateway, err := gatewayFactory()
	if err != nil {
		return protocol.ErrorFor(request, "source_unavailable", "WeRead data is unavailable.", "微信读书数据暂不可用。"), nil
	}
	return weread.NewHandler(gateway).Handle(context.Background(), request), nil
}

func write(response any) {
	encoded, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode response:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "write response:", err)
		os.Exit(1)
	}
}
