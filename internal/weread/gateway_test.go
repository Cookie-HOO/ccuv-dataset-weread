package weread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
)

func TestSourcePeriodMapsNaturalPeriodsToGatewayRequests(t *testing.T) {
	weekly, monthly, annually := "1w", "1mo", "1y"
	tests := []struct {
		name     string
		interval protocol.Range
		mode     string
		baseTime int64
	}{
		{
			name:     "week starts on Monday",
			interval: protocol.Range{StartDate: "2026-09-14", EndDate: "2026-09-19", Period: &weekly},
			mode:     "weekly",
			baseTime: shanghaiUnix(2026, time.September, 14),
		},
		{
			name:     "Monday is its own week start",
			interval: protocol.Range{StartDate: "2026-09-14", EndDate: "2026-09-14", Period: &weekly},
			mode:     "weekly",
			baseTime: shanghaiUnix(2026, time.September, 14),
		},
		{
			name:     "month starts on first day",
			interval: protocol.Range{StartDate: "2026-09-01", EndDate: "2026-09-19", Period: &monthly},
			mode:     "monthly",
			baseTime: shanghaiUnix(2026, time.September, 1),
		},
		{
			name:     "year starts on January first",
			interval: protocol.Range{StartDate: "2026-01-01", EndDate: "2026-09-19", Period: &annually},
			mode:     "annually",
			baseTime: shanghaiUnix(2026, time.January, 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, baseTime, err := sourcePeriod(test.interval)
			if err != nil {
				t.Fatalf("sourcePeriod() error = %v", err)
			}
			if mode != test.mode || baseTime != test.baseTime {
				t.Fatalf("sourcePeriod() = (%q, %d), want (%q, %d)", mode, baseTime, test.mode, test.baseTime)
			}
		})
	}
}

func TestHTTPGatewayWeeklyRankingAnchorsRequestToMonday(t *testing.T) {
	var calls []map[string]any
	server := detailServer(t, &calls, func(map[string]any) string {
		return `{"errcode":0,"readLongest":[]}`
	})
	defer server.Close()

	weekly := "1w"
	_, err := testGateway(server.URL, fixedNow).PeriodRanking(context.Background(), protocol.Range{
		StartDate: "2026-09-14",
		EndDate:   "2026-09-20",
		Period:    &weekly,
	}, "book")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0]["mode"] != "weekly" || calls[0]["baseTime"] != float64(shanghaiUnix(2026, time.September, 14)) {
		t.Fatalf("weekly request = %#v", calls[0])
	}
}

func TestSourcePeriodRejectsMissingUnsupportedAndMismatchedRanges(t *testing.T) {
	unsupported := "14d"
	monthly := "1mo"
	for name, interval := range map[string]protocol.Range{
		"missing period":   {StartDate: "2026-09-01", EndDate: "2026-09-19"},
		"unsupported":      {StartDate: "2026-09-01", EndDate: "2026-09-19", Period: &unsupported},
		"mismatched range": {StartDate: "2026-09-02", EndDate: "2026-09-19", Period: &monthly},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := sourcePeriod(interval); err == nil {
				t.Fatal("sourcePeriod() accepted invalid interval")
			}
		})
	}
}

func TestParseBookAndCategoryRankings(t *testing.T) {
	bookRecords, err := parseBookRanking(json.RawMessage(`[
		{"book":{"bookId":42,"title":"Book One"},"readTime":120},
		{"book":{"bookId":"abc","title":"Book Two"},"readTime":0}
	]`))
	if err != nil {
		t.Fatalf("parseBookRanking() error = %v", err)
	}
	if want := []RankedRecord{{ID: "book:42", Label: "Book One", Seconds: "120"}, {ID: "book:abc", Label: "Book Two", Seconds: "0"}}; !reflect.DeepEqual(bookRecords, want) {
		t.Fatalf("book records = %#v, want %#v", bookRecords, want)
	}

	categoryRecords, err := parseCategoryRanking(json.RawMessage(`[
		{"categoryId":"technology","categoryTitle":"Technology","readingTime":90},
		{"categoryId":7,"categoryTitle":"History","readingTime":180}
	]`))
	if err != nil {
		t.Fatalf("parseCategoryRanking() error = %v", err)
	}
	if want := []RankedRecord{{ID: "category:technology", Label: "Technology", Seconds: "90"}, {ID: "category:7", Label: "History", Seconds: "180"}}; !reflect.DeepEqual(categoryRecords, want) {
		t.Fatalf("category records = %#v, want %#v", categoryRecords, want)
	}
}

func TestParseBookAndCategoryRankingsRejectInvalidFixtures(t *testing.T) {
	tests := []struct {
		name  string
		parse func(json.RawMessage) ([]RankedRecord, error)
		raw   string
	}{
		{"book missing title", parseBookRanking, `[{"book":{"bookId":42},"readTime":120}]`},
		{"book nonnumeric read time", parseBookRanking, `[{"book":{"bookId":42,"title":"Book"},"readTime":"not-a-number"}]`},
		{"category missing ID", parseCategoryRanking, `[{"categoryTitle":"History","readingTime":120}]`},
		{"category nonnumeric read time", parseCategoryRanking, `[{"categoryId":7,"categoryTitle":"History","readingTime":"not-a-number"}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.parse(json.RawMessage(test.raw)); err == nil {
				t.Fatal("parser accepted invalid source fixture")
			}
		})
	}
}

func TestRequestedDatesUseShanghaiCalendarDates(t *testing.T) {
	dates, err := requestedDates([]protocol.Range{{StartDate: "2026-09-19", EndDate: "2026-09-19"}})
	if err != nil || len(dates) != 1 || dates[0].Location() != weReadLocation || dates[0].Format("2006-01-02") != "2026-09-19" {
		t.Fatalf("dates = %#v, error = %v", dates, err)
	}
}

func TestHTTPGatewayPreservesAnnualDailySeconds(t *testing.T) {
	var calls []map[string]any
	server := detailServer(t, &calls, func(body map[string]any) string {
		return `{"errcode":0,"totalReadTime":4081,"dailyReadTimes":{"2026-09-17":1681,"2026-09-18":2400}}`
	})
	defer server.Close()

	records, err := testGateway(server.URL, fixedNow).DailyRecords(context.Background(), []protocol.Range{{StartDate: "2026-09-17", EndDate: "2026-09-18"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Seconds != "1681" || records[1].Seconds != "2400" {
		t.Fatalf("records = %#v", records)
	}
	if len(calls) != 1 || calls[0]["mode"] != "annually" || calls[0]["api_name"] != "/readdata/detail" || calls[0]["skill_version"] != skillVersion {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestHTTPGatewayFallsBackToMonthlyDetail(t *testing.T) {
	var calls []map[string]any
	september17 := shanghaiUnix(2026, time.September, 17)
	september18 := shanghaiUnix(2026, time.September, 18)
	server := detailServer(t, &calls, func(body map[string]any) string {
		if body["mode"] == "annually" {
			return `{"errcode":0,"totalReadTime":120}`
		}
		return `{"errcode":0,"totalReadTime":120,"readTimes":{"` + strconv.FormatInt(september17, 10) + `":60,"` + strconv.FormatInt(september18, 10) + `":60}}`
	})
	defer server.Close()

	records, err := testGateway(server.URL, fixedNow).DailyRecords(context.Background(), []protocol.Range{{StartDate: "2026-09-17", EndDate: "2026-09-18"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{records[0].Seconds, records[1].Seconds}; strings.Join(got, ",") != "60,60" {
		t.Fatalf("records = %#v", records)
	}
	if len(calls) != 2 || calls[0]["mode"] != "annually" || calls[1]["mode"] != "monthly" {
		t.Fatalf("calls = %#v", calls)
	}
	if got := int64(calls[1]["baseTime"].(float64)); got != shanghaiUnix(2026, time.September, 1) {
		t.Fatalf("monthly baseTime = %d", got)
	}
}

func TestHTTPGatewayRequestsOneMonthlyDetailPerNeededMonth(t *testing.T) {
	var calls []map[string]any
	server := detailServer(t, &calls, func(body map[string]any) string {
		if body["mode"] == "annually" {
			return `{"errcode":0,"totalReadTime":0}`
		}
		return `{"errcode":0,"totalReadTime":0,"readTimes":{}}`
	})
	defer server.Close()

	records, err := testGateway(server.URL, fixedNow).DailyRecords(context.Background(), []protocol.Range{{StartDate: "2026-08-31", EndDate: "2026-09-02"}, {StartDate: "2026-09-01", EndDate: "2026-09-02"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %#v", records)
	}
	if len(calls) != 3 || calls[1]["mode"] != "monthly" || calls[2]["mode"] != "monthly" {
		t.Fatalf("calls = %#v", calls)
	}
	got := []int64{int64(calls[1]["baseTime"].(float64)), int64(calls[2]["baseTime"].(float64))}
	want := []int64{shanghaiUnix(2026, time.August, 1), shanghaiUnix(2026, time.September, 1)}
	if strings.Trim(strings.Join([]string{strconv.FormatInt(got[0], 10), strconv.FormatInt(got[1], 10)}, ","), "") != strings.Join([]string{strconv.FormatInt(want[0], 10), strconv.FormatInt(want[1], 10)}, ",") {
		t.Fatalf("monthly baseTimes = %v, want %v", got, want)
	}
}

func TestHTTPGatewayMonthlyCoverageRules(t *testing.T) {
	tests := map[string]struct {
		monthly   string
		date      string
		want      string
		wantErr   error
		wantError bool
	}{
		"omitted completed date reconciles to zero":    {monthly: `{"errcode":0,"totalReadTime":60,"readTimes":{"2026-09-17":60}}`, date: "2026-09-18", want: "0"},
		"explicit zero is accepted":                    {monthly: `{"errcode":0,"totalReadTime":0,"readTimes":{"2026-09-18":0}}`, date: "2026-09-18", want: "0"},
		"unreconciled explicit date remains exact":     {monthly: `{"errcode":0,"totalReadTime":61,"readTimes":{"2026-09-17":60}}`, date: "2026-09-17", want: "60"},
		"unreconciled omission is unavailable":         {monthly: `{"errcode":0,"totalReadTime":61,"readTimes":{"2026-09-17":60}}`, date: "2026-09-18", wantErr: errRangeNotAvailable},
		"missing read times is unavailable":            {monthly: `{"errcode":0,"totalReadTime":0}`, date: "2026-09-18", wantErr: errRangeNotAvailable},
		"out of month record is invalid":               {monthly: `{"errcode":0,"totalReadTime":1,"readTimes":{"2026-10-01":1}}`, date: "2026-09-18", wantError: true},
		"current Shanghai day follows source coverage": {monthly: `{"errcode":0,"totalReadTime":0,"readTimes":{}}`, date: "2026-09-20", want: "0"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := detailServer(t, nil, func(body map[string]any) string {
				if body["mode"] == "annually" {
					return `{"errcode":0,"totalReadTime":0}`
				}
				return test.monthly
			})
			defer server.Close()
			records, err := testGateway(server.URL, fixedNow).DailyRecords(context.Background(), []protocol.Range{{StartDate: test.date, EndDate: test.date}})
			if test.wantError {
				if err == nil || errorsIsRange(err) {
					t.Fatalf("error = %v, want non-coverage error", err)
				}
				return
			}
			if err != test.wantErr {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if err == nil && (len(records) != 1 || records[0].Seconds != test.want) {
				t.Fatalf("records = %#v", records)
			}
		})
	}
}

func TestHTTPGatewayRejectsMalformedSourceData(t *testing.T) {
	for name, monthly := range map[string]string{
		"non numeric":  `{"errcode":0,"totalReadTime":0,"readTimes":{"2026-09-17":"no"}}`,
		"upgrade":      `{"errcode":0,"upgrade_info":{"version":"new"}}`,
		"source error": `{"errcode":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := detailServer(t, nil, func(body map[string]any) string {
				if body["mode"] == "annually" {
					return `{"errcode":0,"totalReadTime":0}`
				}
				return monthly
			})
			defer server.Close()
			_, err := testGateway(server.URL, fixedNow).DailyRecords(context.Background(), []protocol.Range{{StartDate: "2026-09-17", EndDate: "2026-09-17"}})
			if err == nil || errorsIsRange(err) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHTTPGatewayDoesNotExposeSecretsOrRawResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("private upstream data"))
	}))
	defer server.Close()
	gateway := testGateway(server.URL, fixedNow)
	gateway.apiKey = "super-secret"
	_, err := gateway.DailyRecords(context.Background(), []protocol.Range{{StartDate: "2026-09-17", EndDate: "2026-09-17"}})
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "private upstream") {
		t.Fatalf("error = %v", err)
	}
}

func detailServer(t *testing.T, calls *[]map[string]any, response func(map[string]any) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request = %#v", request)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if calls != nil {
			*calls = append(*calls, body)
		}
		_, _ = writer.Write([]byte(response(body)))
	}))
}

func testGateway(target string, _ func() time.Time) HTTPGateway {
	return HTTPGateway{client: &http.Client{Transport: rewriteTransport{target: target}}, apiKey: "test-key", endpoint: gatewayURL}
}

func fixedNow() time.Time { return time.Date(2026, time.September, 20, 8, 0, 0, 0, weReadLocation) }
func shanghaiUnix(year int, month time.Month, day int) int64 {
	return time.Date(year, month, day, 0, 0, 0, 0, weReadLocation).Unix()
}
func errorsIsRange(err error) bool { return err == errRangeNotAvailable }

type rewriteTransport struct{ target string }

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(transport.target, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}
