// Package dataset owns Dataset-wide metadata, Metric routing, and manifests.
package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/metrics/readingtime"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/metrics/totalnotes"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

type Gateway = weread.Gateway
type GatewayFactory func() (Gateway, error)

type Registry struct{ factory GatewayFactory }

func NewRegistry(factory GatewayFactory) Registry { return Registry{factory} }

func Descriptor() v2.ProbeDescriptor {
	text := func(en, zh string) v2.LocalizedText { return v2.LocalizedText{EN: en, ZhCN: zh} }
	return v2.ProbeDescriptor{
		Revision: "weread-3", Version: "0.1.0", SourceTimezone: "Asia/Shanghai", DefaultMetricID: readingtime.ID,
		Dataset:     v2.Dataset{Title: text("WeRead", "微信读书"), Description: text("WeRead reading metrics", "微信读书阅读指标")},
		Metrics:     []v2.MetricDescriptor{readingtime.Descriptor(), totalnotes.Descriptor()},
		Environment: []v2.EnvironmentDeclaration{{Name: "WEREAD_API_KEY", Required: true, Sensitive: true, Label: text("WeRead API key", "微信读书 API 密钥"), HowToGet: text("Export WEREAD_API_KEY in the shell that starts ccuv.", "请在启动 ccuv 的 Shell 中导出 WEREAD_API_KEY。"), HelpURL: "https://weread.qq.com/r/weread-skills"}},
	}
}

func (r Registry) Handle(ctx context.Context, request v2.Request) v2.Response {
	if request.RequestKind == "probe" {
		return v2.OK(request, map[string]any{"descriptor": Descriptor()})
	}
	if request.MetricID != readingtime.ID && request.MetricID != totalnotes.ID {
		return v2.ErrorFor(request, "unsupported_chart_kind", "WeRead does not support the selected Metric.", "微信读书不支持所选指标。", nil)
	}
	if request.MetricID == readingtime.ID {
		if request.RequestKind == "chart" {
			if response := readingtime.ValidateChart(request, *request.Chart); response != nil {
				return *response
			}
		} else if response := readingtime.ValidateSummary(request); response != nil {
			return *response
		}
	} else if request.RequestKind == "chart" {
		if response := totalnotes.ValidateChart(request, *request.Chart); response != nil {
			return *response
		}
	} else if response := totalnotes.ValidateSummary(request); response != nil {
		return *response
	}
	gateway, err := r.factory()
	if err != nil {
		return sourceUnavailable(request, err)
	}
	var response v2.Response
	switch request.MetricID {
	case readingtime.ID:
		handler := readingtime.New(gateway)
		if request.RequestKind == "chart" {
			response = handler.Chart(ctx, request)
			break
		}
		return handler.Summary(ctx, request)
	case totalnotes.ID:
		handler := totalnotes.New(gateway)
		if request.RequestKind == "chart" {
			response = handler.Chart(ctx, request)
			break
		}
		return handler.Summary(ctx, request)
	default:
		panic("validated Metric ID was not routed")
	}
	if response.Status == "ok" {
		response.Payload["descriptor"] = Descriptor()
		response.Payload["runtime"] = map[string]any{"filters": []any{}}
		delete(response.Payload, "dataset")
		delete(response.Payload, "capabilities")
	}
	return response
}

func sourceUnavailable(request v2.Request, err error) v2.Response {
	en, zh := "WeRead data is unavailable.", "微信读书数据暂不可用。"
	if err != nil && err.Error() == "WEREAD_API_KEY is not configured" {
		en, zh = "WEREAD_API_KEY is not configured. Export it in the shell that starts ccuv.", "未配置 WEREAD_API_KEY。请在启动 ccuv 的 Shell 中导出该环境变量。"
	}
	return v2.ErrorFor(request, "source_unavailable", en, zh, nil)
}

type ManifestProbe struct {
	DatasetID  string             `json:"dataset_id"`
	Descriptor v2.ProbeDescriptor `json:"descriptor"`
}

type Manifest struct {
	Schema     string `json:"schema"`
	Protocol   string `json:"protocol"`
	Entrypoint string `json:"entrypoint"`
	Platform   struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	} `json:"platform"`
	Probe ManifestProbe `json:"probe"`
}

func BuildManifest(os, arch string) (Manifest, error) {
	if os == "" || arch == "" {
		return Manifest{}, fmt.Errorf("platform is required")
	}
	entrypoint := "bin/ccuv-dataset-weread"
	if os == "windows" {
		entrypoint += ".exe"
	}
	manifest := Manifest{Schema: "ccuv.dataset/v1", Protocol: v2.Version, Entrypoint: entrypoint, Probe: ManifestProbe{DatasetID: v2.DatasetID, Descriptor: Descriptor()}}
	manifest.Platform.OS, manifest.Platform.Arch = os, arch
	return manifest, nil
}

func WriteManifest(writer io.Writer, os, arch string) error {
	manifest, err := BuildManifest(os, arch)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	_, err = writer.Write(append(encoded, '\n'))
	return err
}
