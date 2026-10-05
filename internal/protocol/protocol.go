// Package protocol implements the strict ccuv.custom/v1 wire contract.
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const Version = "ccuv.custom/v1"
const DatasetID = "weread"

// ccuv allocates request IDs as <pane-owner>:<generation>; dataset IDs remain
// separately fixed to DatasetID and therefore do not inherit this broader form.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-:][a-z0-9]+)*$`)

// LocalizedText is text supplied in every language the ccuv client supports.
type LocalizedText struct {
	EN   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

// Range is an inclusive calendar-date interval.
type Range struct {
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
	Period    *string `json:"period"`
}

// Query describes the requested aggregation. This dataset only permits daily totals.
type Query struct {
	Filters     map[string][]string `json:"filters"`
	GroupBy     *string             `json:"group_by"`
	Granularity string              `json:"granularity"`
}

// Summary describes ccuv-resolved comparison intervals.
type Summary struct {
	Scope     string           `json:"scope"`
	Intervals map[string]Range `json:"intervals"`
}

// Request is the sole stdin document accepted by this command.
type Request struct {
	Protocol    string   `json:"protocol"`
	RequestID   string   `json:"request_id"`
	RequestKind string   `json:"request_kind"`
	DatasetID   string   `json:"dataset_id"`
	Timezone    *string  `json:"timezone"`
	Range       *Range   `json:"range,omitempty"`
	Query       Query    `json:"query"`
	Summary     *Summary `json:"summary,omitempty"`
}

func DecodeRequest(reader io.Reader) (Request, error) {
	input, err := io.ReadAll(reader)
	if err != nil {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	return DecodeRequestBytes(input)
}

// DecodeRequestBytes strictly decodes the sole request document.
func DecodeRequestBytes(input []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Request{}, fmt.Errorf("request must contain exactly one JSON value")
	}
	return request, nil
}

// InvalidRequestFor returns a correlated invalid-request response only when the
// single JSON document contains a trustworthy request envelope. Unrecoverable
// input must not receive a fabricated protocol response.
func InvalidRequestFor(input []byte) (ErrorResponse, bool) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	var envelope struct {
		Protocol    string `json:"protocol"`
		RequestID   string `json:"request_id"`
		RequestKind string `json:"request_kind"`
		DatasetID   string `json:"dataset_id"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return ErrorResponse{}, false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ErrorResponse{}, false
	}
	if envelope.Protocol != Version || !idPattern.MatchString(envelope.RequestID) || envelope.DatasetID != DatasetID || (envelope.RequestKind != "chart" && envelope.RequestKind != "summary") {
		return ErrorResponse{}, false
	}
	return ErrorFor(
		Request{
			Protocol: envelope.Protocol, RequestID: envelope.RequestID,
			RequestKind: envelope.RequestKind, DatasetID: envelope.DatasetID,
		},
		"invalid_request",
		"The local WeRead command could not decode this request. Update it to a version compatible with ccuv.",
		"本地微信读书命令无法解析此请求。请更新到与 ccuv 兼容的版本。",
	), true
}

// Validate verifies envelope and shape before source-specific handling.
func (r Request) Validate() error {
	if r.Protocol != Version {
		return fmt.Errorf("unsupported protocol")
	}
	if !idPattern.MatchString(r.RequestID) {
		return fmt.Errorf("invalid request_id")
	}
	if r.DatasetID != DatasetID {
		return fmt.Errorf("unsupported dataset")
	}
	if r.RequestKind != "chart" && r.RequestKind != "summary" {
		return fmt.Errorf("invalid request_kind")
	}
	if r.Timezone == nil || *r.Timezone == "" {
		return fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(*r.Timezone); err != nil {
		return fmt.Errorf("invalid timezone")
	}
	if err := r.Query.validate(r.RequestKind == "chart"); err != nil {
		return err
	}
	if r.RequestKind == "chart" {
		if r.Range == nil || r.Summary != nil {
			return fmt.Errorf("chart request must include range and omit summary")
		}
		if r.Query.Granularity != "day" {
			return fmt.Errorf("only daily granularity is supported")
		}
		return r.Range.validate()
	}
	if r.Range != nil || r.Summary == nil {
		return fmt.Errorf("summary request must include summary and omit range")
	}
	return r.Summary.validate()
}

func (q Query) validate(isChart bool) error {
	if q.Filters == nil {
		return fmt.Errorf("query.filters is required")
	}
	if len(q.Filters) != 0 {
		return fmt.Errorf("filters are not supported")
	}
	if isChart && q.Granularity != "day" {
		return fmt.Errorf("only daily granularity is supported")
	}
	if !isChart && q.Granularity != "" && q.Granularity != "day" {
		return fmt.Errorf("only daily granularity is supported")
	}
	return nil
}

func (r Range) validate() error {
	start, err := parseDate(r.StartDate)
	if err != nil {
		return fmt.Errorf("invalid start_date")
	}
	end, err := parseDate(r.EndDate)
	if err != nil || end.Before(start) {
		return fmt.Errorf("invalid end_date")
	}
	if r.Period != nil && !validPeriod(*r.Period) {
		return fmt.Errorf("invalid period")
	}
	return nil
}

func validPeriod(value string) bool {
	match := regexp.MustCompile(`^([1-9][0-9]*)(d|w|mo|q|y)$`).FindStringSubmatch(value)
	if match == nil {
		return false
	}
	_, err := strconv.ParseUint(match[1], 10, 64)
	return err == nil
}
func (s Summary) validate() error {
	if s.Scope == "" || len(s.Intervals) == 0 {
		return fmt.Errorf("invalid summary")
	}
	if _, ok := s.Intervals["current"]; !ok {
		return fmt.Errorf("summary requires current interval")
	}
	for name, interval := range s.Intervals {
		if name != "current" && name != "sequential" && name != "year_over_year" {
			return fmt.Errorf("unknown summary interval")
		}
		if err := interval.validate(); err != nil {
			return fmt.Errorf("invalid %s interval", name)
		}
	}
	return nil
}
func parseDate(value string) (time.Time, error) { return time.Parse("2006-01-02", value) }

// ErrorResponse is a well-formed protocol failure.
type ErrorResponse struct {
	Protocol    string        `json:"protocol"`
	RequestID   string        `json:"request_id"`
	RequestKind string        `json:"request_kind"`
	DatasetID   string        `json:"dataset_id"`
	Status      string        `json:"status"`
	Error       ResponseError `json:"error"`
}
type ResponseError struct {
	Code        string        `json:"code"`
	Message     LocalizedText `json:"message"`
	Unsupported *Unsupported  `json:"unsupported,omitempty"`
}

type Unsupported struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

func ErrorFor(request Request, code, en, zhCN string) ErrorResponse {
	return ErrorResponse{Version, request.RequestID, request.RequestKind, request.DatasetID, "error", ResponseError{Code: code, Message: LocalizedText{en, zhCN}}}
}

func UnsupportedQueryFor(request Request, field, value, en, zhCN string) ErrorResponse {
	return ErrorResponse{Version, request.RequestID, request.RequestKind, request.DatasetID, "error", ResponseError{Code: "unsupported_query", Message: LocalizedText{en, zhCN}, Unsupported: &Unsupported{field, value}}}
}

func InvalidRequest() ErrorResponse {
	return ErrorResponse{Version, "invalid-request", "chart", DatasetID, "error", ResponseError{Code: "invalid_request", Message: LocalizedText{"The request is invalid.", "请求无效。"}}}
}

// ChartResponse is the strictly additive daily-total response.
type Warning struct {
	Code    string        `json:"code"`
	Message LocalizedText `json:"message"`
}

type ChartResponse struct {
	Protocol     string       `json:"protocol"`
	RequestID    string       `json:"request_id"`
	RequestKind  string       `json:"request_kind"`
	DatasetID    string       `json:"dataset_id"`
	Status       string       `json:"status"`
	Warnings     []Warning    `json:"warnings,omitempty"`
	Dataset      Dataset      `json:"dataset"`
	Capabilities Capabilities `json:"capabilities"`
	Data         Data         `json:"data"`
}
type Dataset struct {
	Title       LocalizedText `json:"title"`
	Description LocalizedText `json:"description"`
	Unit        Unit          `json:"unit"`
}
type Unit struct {
	ID            string        `json:"id"`
	Label         LocalizedText `json:"label"`
	Placement     string        `json:"placement"`
	DecimalPlaces int           `json:"decimal_places"`
}
type Capabilities struct {
	GroupBy []any                      `json:"group_by"`
	Filters []any                      `json:"filters"`
	Charts  map[string]ChartCapability `json:"charts"`
}
type ChartCapability struct {
	Granularities []string `json:"granularities"`
	GroupBy       []string `json:"group_by"`
	Filters       []string `json:"filters"`
}
type Data struct {
	Series []Series `json:"series"`
}

var RankingGroupBy = []string{"book", "category"}

type Series struct {
	ID     string        `json:"id"`
	Label  LocalizedText `json:"label"`
	Points []Point       `json:"points"`
}
type Point struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

func NewChartResponse(request Request, points []Point) ChartResponse {
	charts := map[string]ChartCapability{}
	for _, kind := range []string{"timeline", "calendar", "stack"} {
		charts[kind] = ChartCapability{[]string{"day"}, []string{}, []string{}}
	}
	charts["ranking"] = ChartCapability{[]string{"day"}, rankingGroupBy(), []string{}}
	return ChartResponse{Version, request.RequestID, "chart", DatasetID, "ok", timezoneWarnings(request), Dataset{
		LocalizedText{"WeRead", "微信读书"}, LocalizedText{"Daily reading time", "每日阅读时长"}, Unit{"minute", LocalizedText{"minutes", "分钟"}, "suffix", 2},
	}, Capabilities{groupDimensions(), []any{}, charts}, Data{[]Series{{"total", LocalizedText{"Total", "总计"}, points}}}}
}

func NewGroupedRankingResponse(request Request, series []Series) ChartResponse {
	return ChartResponse{Version, request.RequestID, "chart", DatasetID, "ok", timezoneWarnings(request), Dataset{
		LocalizedText{"WeRead", "微信读书"}, LocalizedText{"Reading time", "阅读时长"}, Unit{"minute", LocalizedText{"minutes", "分钟"}, "suffix", 2},
	}, Capabilities{groupDimensions(), []any{}, map[string]ChartCapability{
		"ranking": {[]string{"day"}, rankingGroupBy(), []string{}},
	}}, Data{series}}
}

func rankingGroupBy() []string {
	return append([]string(nil), RankingGroupBy...)
}

func groupDimensions() []any {
	return []any{
		map[string]any{"id": "book", "label": LocalizedText{"Book", "书籍"}},
		map[string]any{"id": "category", "label": LocalizedText{"Category", "分类"}},
	}
}

func timezoneWarnings(request Request) []Warning {
	if request.Timezone == nil || *request.Timezone == "Asia/Shanghai" {
		return nil
	}
	return []Warning{{"timezone_not_supported", LocalizedText{
		"WeRead dates use the Asia/Shanghai source timezone.",
		"微信读书日期使用 Asia/Shanghai 数据源时区。",
	}}}
}

type SummaryResponse struct {
	Protocol    string        `json:"protocol"`
	RequestID   string        `json:"request_id"`
	RequestKind string        `json:"request_kind"`
	DatasetID   string        `json:"dataset_id"`
	Status      string        `json:"status"`
	Warnings    []Warning     `json:"warnings,omitempty"`
	Summary     SummaryResult `json:"summary"`
}
type SummaryResult struct {
	Metric SummaryMetric `json:"metric"`
}
type SummaryMetric struct {
	ID           string         `json:"id"`
	Label        LocalizedText  `json:"label"`
	Current      SummaryCurrent `json:"current"`
	Sequential   Comparison     `json:"sequential"`
	YearOverYear Comparison     `json:"year_over_year"`
}
type SummaryCurrent struct {
	Value   string        `json:"value"`
	Display LocalizedText `json:"display"`
}
type Comparison struct {
	Status    string  `json:"status"`
	BaseValue *string `json:"base_value,omitempty"`
}

func NewSummaryResponse(request Request, current string, sequential, yearOverYear *string) SummaryResponse {
	comparison := func(value *string) Comparison {
		if value == nil {
			return Comparison{Status: "unavailable"}
		}
		return Comparison{Status: "ready", BaseValue: value}
	}
	return SummaryResponse{Version, request.RequestID, "summary", DatasetID, "ok", timezoneWarnings(request), SummaryResult{SummaryMetric{"reading-time", LocalizedText{"Reading time", "阅读时长"}, SummaryCurrent{current, LocalizedText{current + " minutes", current + " 分钟"}}, comparison(sequential), comparison(yearOverYear)}}}
}

func Encode(response any) ([]byte, error) { return json.Marshal(response) }
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	return marshalCanonical(decoded)
}
func marshalCanonical(value any) ([]byte, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var b bytes.Buffer
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(key)
			b.Write(k)
			b.WriteByte(':')
			child, err := marshalCanonical(v[key])
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			child, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}
