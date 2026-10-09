// Package v2 implements the ccuv.custom/v2 request envelope.
package v2

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

const Version = "ccuv.custom/v2"
const DatasetID = "weread"

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-:][a-z0-9]+)*$`)
var metricIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[_-][a-z0-9]+)*$`)

type Range struct {
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
	Period    *string `json:"period"`
}

type Query struct {
	Filters     map[string][]string `json:"filters"`
	GroupBy     *string             `json:"group_by"`
	Granularity string              `json:"granularity"`
	Top         *int                `json:"top"`
}

type Summary struct {
	Scope     string           `json:"scope"`
	Intervals map[string]Range `json:"intervals"`
}

type ChartRequest struct {
	Range Range `json:"range"`
	Query Query `json:"query"`
}

type SummaryRequest struct {
	Query   Query   `json:"query"`
	Summary Summary `json:"summary"`
}

type Request struct {
	Protocol    string
	RequestID   string
	RequestKind string
	DatasetID   string
	MetricID    string
	Timezone    string
	Chart       *ChartRequest
	Summary     *SummaryRequest
}

type requestEnvelope struct {
	Protocol    string          `json:"protocol"`
	RequestID   string          `json:"request_id"`
	RequestKind string          `json:"request_kind"`
	DatasetID   string          `json:"dataset_id"`
	MetricID    string          `json:"metric_id"`
	Timezone    string          `json:"timezone"`
	Range       json.RawMessage `json:"range"`
	Query       json.RawMessage `json:"query"`
	Summary     json.RawMessage `json:"summary"`
}

type LocalizedText struct {
	EN   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

type Unit struct {
	ID            string        `json:"id"`
	Label         LocalizedText `json:"label"`
	Placement     string        `json:"placement"`
	DecimalPlaces int           `json:"decimal_places"`
}

type Dataset struct {
	Title       LocalizedText `json:"title"`
	Description LocalizedText `json:"description"`
}

type Dimension struct {
	ID    string        `json:"id"`
	Label LocalizedText `json:"label"`
}

type ChartCapability struct {
	Granularities []string `json:"granularities"`
	GroupBy       []string `json:"group_by"`
	Periods       []string `json:"periods,omitempty"`
	MaxItems      *int     `json:"max_items,omitempty"`
}

type MetricCapabilities struct {
	GroupBy []Dimension                `json:"group_by"`
	Charts  map[string]ChartCapability `json:"charts"`
}

type ChartDefaults struct {
	Period  any `json:"period"`
	GroupBy any `json:"group_by"`
	Top     any `json:"top"`
}

type MetricDescriptor struct {
	ID           string                   `json:"id"`
	Title        LocalizedText            `json:"title"`
	Description  LocalizedText            `json:"description"`
	Unit         Unit                     `json:"unit"`
	Aggregation  string                   `json:"aggregation"`
	Summary      string                   `json:"summary"`
	Capabilities MetricCapabilities       `json:"capabilities"`
	Defaults     map[string]ChartDefaults `json:"defaults"`
}

type EnvironmentDeclaration struct {
	Name      string        `json:"name"`
	Required  bool          `json:"required"`
	Sensitive bool          `json:"sensitive"`
	Label     LocalizedText `json:"label"`
	HowToGet  LocalizedText `json:"how_to_get"`
	HelpURL   string        `json:"help_url,omitempty"`
}

type ProbeDescriptor struct {
	Revision        string                   `json:"revision"`
	Version         string                   `json:"version"`
	SourceTimezone  string                   `json:"source_timezone"`
	DefaultMetricID string                   `json:"default_metric_id"`
	Dataset         Dataset                  `json:"dataset"`
	Metrics         []MetricDescriptor       `json:"metrics"`
	Environment     []EnvironmentDeclaration `json:"environment"`
}

type Warning struct {
	Code    string        `json:"code"`
	Message LocalizedText `json:"message"`
}

type Point struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type Series struct {
	ID     string        `json:"id"`
	Label  LocalizedText `json:"label"`
	Points []Point       `json:"points"`
}

type Unsupported struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

type ResponseError struct {
	Code        string        `json:"code"`
	Message     LocalizedText `json:"message"`
	Unsupported *Unsupported  `json:"unsupported,omitempty"`
}

// Response deliberately retains payloads as JSON values. Metric packages own
// their result payload while this package owns the correlated envelope.
type Response struct {
	Protocol    string         `json:"protocol"`
	RequestID   string         `json:"request_id"`
	RequestKind string         `json:"request_kind"`
	DatasetID   string         `json:"dataset_id"`
	MetricID    string         `json:"metric_id,omitempty"`
	Status      string         `json:"status"`
	Error       *ResponseError `json:"error,omitempty"`
	Payload     map[string]any `json:"-"`
}

func (r Response) MarshalJSON() ([]byte, error) {
	value := map[string]any{
		"protocol": r.Protocol, "request_id": r.RequestID, "request_kind": r.RequestKind,
		"dataset_id": r.DatasetID, "status": r.Status,
	}
	if r.MetricID != "" {
		value["metric_id"] = r.MetricID
	}
	if r.Error != nil {
		value["error"] = r.Error
	}
	for key, item := range r.Payload {
		value[key] = item
	}
	return json.Marshal(value)
}

func Decode(input []byte) (Request, error) {
	var root requestEnvelope
	if err := json.Unmarshal(input, &root); err != nil {
		return Request{}, fmt.Errorf("request must contain exactly one JSON value")
	}
	request := Request{root.Protocol, root.RequestID, root.RequestKind, root.DatasetID, root.MetricID, root.Timezone, nil, nil}
	if err := validateEnvelope(request); err != nil {
		return Request{}, err
	}
	switch request.RequestKind {
	case "probe":
		if request.DatasetID != "" || request.MetricID != "" || request.Timezone != "" || len(root.Range) != 0 || len(root.Query) != 0 || len(root.Summary) != 0 {
			return Request{}, fmt.Errorf("probe must omit Dataset query fields")
		}
	case "chart":
		var chart ChartRequest
		if err := json.Unmarshal(root.Range, &chart.Range); err != nil {
			return Request{}, fmt.Errorf("chart request is invalid")
		}
		if err := json.Unmarshal(root.Query, &chart.Query); err != nil || len(root.Summary) != 0 {
			return Request{}, fmt.Errorf("chart request is invalid")
		}
		if err := validateChart(request, chart); err != nil {
			return Request{}, err
		}
		request.Chart = &chart
	case "summary":
		var summary SummaryRequest
		if err := json.Unmarshal(root.Query, &summary.Query); err != nil {
			return Request{}, fmt.Errorf("summary request is invalid")
		}
		if err := json.Unmarshal(root.Summary, &summary.Summary); err != nil || len(root.Range) != 0 {
			return Request{}, fmt.Errorf("summary request is invalid")
		}
		if err := validateSummary(request, summary); err != nil {
			return Request{}, err
		}
		request.Summary = &summary
	default:
		return Request{}, fmt.Errorf("unsupported request kind")
	}
	return request, nil
}

func validateEnvelope(request Request) error {
	if request.Protocol != Version || !idPattern.MatchString(request.RequestID) {
		return fmt.Errorf("invalid V2 envelope")
	}
	if request.RequestKind == "probe" {
		return nil
	}
	if request.DatasetID != DatasetID || !metricIDPattern.MatchString(request.MetricID) || request.Timezone == "" {
		return fmt.Errorf("invalid Dataset envelope")
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return fmt.Errorf("invalid timezone")
	}
	return nil
}

func validateChart(request Request, chart ChartRequest) error {
	if chart.Query.Filters == nil || chart.Query.Granularity == "" || chart.Range.StartDate == "" || chart.Range.EndDate == "" {
		return fmt.Errorf("chart fields are required")
	}
	start, err := time.Parse("2006-01-02", chart.Range.StartDate)
	end, endErr := time.Parse("2006-01-02", chart.Range.EndDate)
	if err != nil || endErr != nil || end.Before(start) {
		return fmt.Errorf("invalid chart range")
	}
	if chart.Query.Top != nil && *chart.Query.Top < 1 {
		return fmt.Errorf("invalid chart top")
	}
	return nil
}

func validateSummary(request Request, summary SummaryRequest) error {
	if summary.Query.Filters == nil || summary.Summary.Scope == "" || len(summary.Summary.Intervals) == 0 {
		return fmt.Errorf("summary fields are required")
	}
	if _, ok := summary.Summary.Intervals["current"]; !ok {
		return fmt.Errorf("summary requires current interval")
	}
	return nil
}

func InvalidRequestFor(input []byte) (Response, bool) {
	var envelope requestEnvelope
	if json.Unmarshal(input, &envelope) != nil || envelope.Protocol != Version || !idPattern.MatchString(envelope.RequestID) || (envelope.RequestKind != "chart" && envelope.RequestKind != "summary") || envelope.DatasetID != DatasetID || !metricIDPattern.MatchString(envelope.MetricID) {
		return Response{}, false
	}
	return ErrorFor(Request{RequestID: envelope.RequestID, RequestKind: envelope.RequestKind, DatasetID: DatasetID, MetricID: envelope.MetricID}, "invalid_request", "The request is invalid.", "请求无效。", nil), true
}

func ErrorFor(request Request, code, en, zh string, unsupported *Unsupported) Response {
	return Response{Version, request.RequestID, request.RequestKind, DatasetID, request.MetricID, "error", &ResponseError{code, LocalizedText{en, zh}, unsupported}, nil}
}

func OK(request Request, payload map[string]any) Response {
	return Response{Version, request.RequestID, request.RequestKind, DatasetID, request.MetricID, "ok", nil, payload}
}

func Encode(response Response) ([]byte, error) { return json.Marshal(response) }
