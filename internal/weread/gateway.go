package weread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
)

const (
	gatewayURL       = "https://i.weread.qq.com/api/agent/gateway"
	skillVersion     = "1.0.4"
	maxResponseBytes = 1 << 20
)

var weReadLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}()

var errRangeNotAvailable = errors.New("requested daily range is not available")

type sourceFailureKind string

const (
	sourceFailurePeriod   sourceFailureKind = "period"
	sourceFailureRequest  sourceFailureKind = "request"
	sourceFailureUpgrade  sourceFailureKind = "upgrade"
	sourceFailureResponse sourceFailureKind = "response"
	sourceFailureRanking  sourceFailureKind = "ranking"
)

type sourceFailure struct{ kind sourceFailureKind }

func (failure sourceFailure) Error() string { return string(failure.kind) }

func newSourceFailure(kind sourceFailureKind) error { return sourceFailure{kind: kind} }

func sourceFailureKindOf(err error) sourceFailureKind {
	var failure sourceFailure
	if errors.As(err, &failure) {
		return failure.kind
	}
	return sourceFailureRequest
}

// HTTPGateway obtains exact daily records from WeRead detail responses. Annual
// dailyReadTimes is preferred when available; monthly readTimes is the verified
// fallback. It never derives a day's value from an aggregate total.
type HTTPGateway struct {
	client   *http.Client
	apiKey   string
	endpoint string
}

// NewHTTPGateway obtains its only credential from the inherited environment.
// The key is never stored on disk or written to diagnostics.
func NewHTTPGatewayFromEnvironment() (HTTPGateway, error) {
	apiKey, ok := os.LookupEnv("WEREAD_API_KEY")
	if !ok || strings.TrimSpace(apiKey) == "" {
		return HTTPGateway{}, fmt.Errorf("WEREAD_API_KEY is not configured")
	}
	return HTTPGateway{
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		apiKey: apiKey, endpoint: gatewayURL,
	}, nil
}

func (g HTTPGateway) PeriodRanking(ctx context.Context, interval protocol.Range, groupBy string) ([]RankedRecord, error) {
	if g.client == nil || g.endpoint != gatewayURL || strings.TrimSpace(g.apiKey) == "" {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	mode, baseTime, err := sourcePeriod(interval)
	if err != nil {
		return nil, newSourceFailure(sourceFailurePeriod)
	}
	payload, err := g.fetchDetail(ctx, mode, baseTime)
	if err != nil {
		return nil, err
	}
	response, err := parseDetailResponse(payload)
	if err != nil {
		return nil, err
	}
	var records []RankedRecord
	switch groupBy {
	case "book":
		records, err = parseBookRanking(response.ReadLongest)
	case "category":
		records, err = parseCategoryRanking(response.PreferCategory)
	default:
		return nil, newSourceFailure(sourceFailureRanking)
	}
	if err != nil {
		return nil, newSourceFailure(sourceFailureRanking)
	}
	return records, nil
}

func (g HTTPGateway) DailyRecords(ctx context.Context, intervals []protocol.Range) ([]DailyRecord, error) {
	if g.client == nil || g.endpoint != gatewayURL || strings.TrimSpace(g.apiKey) == "" {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	dates, err := requestedDates(intervals)
	if err != nil {
		return nil, err
	}
	years := map[int]struct{}{}
	for _, date := range dates {
		years[date.Year()] = struct{}{}
	}
	annual := make(map[int]dailyReadTimes, len(years))
	for _, year := range sortedYears(years) {
		readTimes, err := g.annualDailyReadTimes(ctx, year)
		if err != nil {
			return nil, err
		}
		annual[year] = readTimes
	}

	monthly := map[month]dailyReadTimes{}
	records := make([]DailyRecord, 0, len(dates))
	for _, date := range dates {
		key := date.Format("2006-01-02")
		readTimes := annual[date.Year()]
		seconds, known := readTimes.Daily[key]
		if !known && !readTimes.ZeroFillAllowed {
			monthKey := month{year: date.Year(), value: date.Month()}
			monthlyReadTimes, cached := monthly[monthKey]
			if !cached {
				monthlyReadTimes, err = g.monthlyReadTimes(ctx, monthKey)
				if err != nil {
					return nil, err
				}
				monthly[monthKey] = monthlyReadTimes
			}
			seconds, known = monthlyReadTimes.Daily[key]
			if !known && !monthlyReadTimes.ZeroFillAllowed {
				return nil, errRangeNotAvailable
			}
		}
		// A reconciled source total proves that an omitted past bucket is zero.
		records = append(records, DailyRecord{Date: key, Seconds: strconv.FormatUint(seconds, 10)})
	}
	return records, nil
}

type month struct {
	year  int
	value time.Month
}

type detailResponse struct {
	ErrCode        *int                       `json:"errcode"`
	UpgradeInfo    json.RawMessage            `json:"upgrade_info"`
	DailyReadTimes map[string]json.RawMessage `json:"dailyReadTimes"`
	ReadTimes      map[string]json.RawMessage `json:"readTimes"`
	TotalReadTime  json.RawMessage            `json:"totalReadTime"`
	ReadLongest    json.RawMessage            `json:"readLongest"`
	PreferCategory json.RawMessage            `json:"preferCategory"`
}

type dailyReadTimes struct {
	Daily           map[string]uint64
	ZeroFillAllowed bool
}

func (g HTTPGateway) annualDailyReadTimes(ctx context.Context, year int) (dailyReadTimes, error) {
	payload, err := g.fetchDetail(ctx, "annually", time.Date(year, time.January, 1, 0, 0, 0, 0, weReadLocation).Unix())
	if err != nil {
		return dailyReadTimes{}, err
	}
	response, err := parseDetailResponse(payload)
	if err != nil {
		return dailyReadTimes{}, err
	}
	if response.DailyReadTimes == nil {
		return dailyReadTimes{}, nil
	}
	readTimes, err := parseDailyReadTimes(response.DailyReadTimes, response.TotalReadTime, func(date time.Time) bool {
		return date.Year() == year
	})
	// Annual detail is only an optimization. An unreconciled aggregate cannot
	// prove omitted days are zero, but its explicit daily buckets remain exact.
	return readTimes, err
}

func (g HTTPGateway) monthlyReadTimes(ctx context.Context, target month) (dailyReadTimes, error) {
	payload, err := g.fetchDetail(ctx, "monthly", time.Date(target.year, target.value, 1, 0, 0, 0, 0, weReadLocation).Unix())
	if err != nil {
		return dailyReadTimes{}, err
	}
	response, err := parseDetailResponse(payload)
	if err != nil {
		return dailyReadTimes{}, err
	}
	if response.ReadTimes == nil {
		return dailyReadTimes{}, errRangeNotAvailable
	}
	return parseDailyReadTimes(response.ReadTimes, response.TotalReadTime, func(date time.Time) bool {
		return date.Year() == target.year && date.Month() == target.value
	})
}

func (g HTTPGateway) fetchDetail(ctx context.Context, mode string, baseTime int64) ([]byte, error) {
	body, err := json.Marshal(map[string]any{
		"api_name": "/readdata/detail", "mode": mode, "baseTime": baseTime, "skill_version": skillVersion,
	})
	if err != nil {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	request.Header.Set("Authorization", "Bearer "+g.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes {
		return nil, newSourceFailure(sourceFailureResponse)
	}
	return payload, nil
}

func parseDetailResponse(payload []byte) (detailResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var response detailResponse
	if err := decoder.Decode(&response); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return detailResponse{}, newSourceFailure(sourceFailureResponse)
	}
	if len(response.UpgradeInfo) != 0 && string(response.UpgradeInfo) != "null" {
		return detailResponse{}, newSourceFailure(sourceFailureUpgrade)
	}
	if response.ErrCode != nil && *response.ErrCode != 0 {
		return detailResponse{}, newSourceFailure(sourceFailureRequest)
	}
	return response, nil
}

func parseDailyReadTimes(raw map[string]json.RawMessage, rawTotal json.RawMessage, accepts func(time.Time) bool) (dailyReadTimes, error) {
	total, err := parseSeconds(rawTotal)
	if err != nil {
		return dailyReadTimes{}, errors.New("source response is invalid")
	}
	daily := make(map[string]uint64, len(raw))
	var sum uint64
	for rawDate, rawSeconds := range raw {
		date, err := sourceDate(rawDate)
		if err != nil || !accepts(date) {
			return dailyReadTimes{}, errors.New("source response is invalid")
		}
		seconds, err := parseSeconds(rawSeconds)
		if err != nil {
			return dailyReadTimes{}, errors.New("source response is invalid")
		}
		key := date.Format("2006-01-02")
		if _, duplicate := daily[key]; duplicate || ^uint64(0)-sum < seconds {
			return dailyReadTimes{}, errors.New("source response is invalid")
		}
		daily[key] = seconds
		sum += seconds
	}
	// An exact source bucket remains usable even when its aggregate cannot
	// reconcile. Only a reconciled aggregate can prove omitted buckets are zero.
	return dailyReadTimes{Daily: daily, ZeroFillAllowed: sum == total}, nil
}

func requestedDates(intervals []protocol.Range) ([]time.Time, error) {
	requested := map[string]time.Time{}
	for _, interval := range intervals {
		start, err := time.ParseInLocation("2006-01-02", interval.StartDate, weReadLocation)
		if err != nil {
			return nil, err
		}
		end, err := time.ParseInLocation("2006-01-02", interval.EndDate, weReadLocation)
		if err != nil || end.Before(start) {
			return nil, errors.New("invalid range")
		}
		for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
			requested[date.Format("2006-01-02")] = date
		}
	}
	dates := make([]time.Time, 0, len(requested))
	for _, date := range requested {
		dates = append(dates, date)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	return dates, nil
}

func sortedYears(years map[int]struct{}) []int {
	values := make([]int, 0, len(years))
	for year := range years {
		values = append(values, year)
	}
	sort.Ints(values)
	return values
}

func sourceDate(value string) (time.Time, error) {
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(timestamp, 0).In(weReadLocation), nil
	}
	return time.ParseInLocation("2006-01-02", value, weReadLocation)
}

func sourcePeriod(interval protocol.Range) (string, int64, error) {
	if interval.Period == nil {
		return "", 0, errors.New("missing source period")
	}
	end, err := time.ParseInLocation("2006-01-02", interval.EndDate, weReadLocation)
	if err != nil {
		return "", 0, err
	}
	var start time.Time
	var mode string
	switch *interval.Period {
	case "1w":
		mode = "weekly"
		// WeRead weekly detail periods are Monday-based, unlike time.Weekday's
		// Sunday-based numbering.
		start = end.AddDate(0, 0, -(int(end.Weekday())+6)%7)
	case "1mo":
		mode = "monthly"
		start = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, weReadLocation)
	case "1y":
		mode = "annually"
		start = time.Date(end.Year(), time.January, 1, 0, 0, 0, 0, weReadLocation)
	default:
		return "", 0, errors.New("unsupported source period")
	}
	if interval.StartDate != start.Format("2006-01-02") {
		return "", 0, errors.New("source period does not match date range")
	}
	return mode, start.Unix(), nil
}

type sourceBook struct {
	Book struct {
		ID    json.RawMessage `json:"bookId"`
		Title string          `json:"title"`
	} `json:"book"`
	ReadTime json.RawMessage `json:"readTime"`
}

type sourceCategory struct {
	ID          json.RawMessage `json:"categoryId"`
	Title       string          `json:"categoryTitle"`
	ReadingTime json.RawMessage `json:"readingTime"`
}

func parseBookRanking(raw json.RawMessage) ([]RankedRecord, error) {
	var values []sourceBook
	if len(raw) == 0 || string(raw) == "null" {
		return []RankedRecord{}, nil
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("source response is invalid")
	}
	records := make([]RankedRecord, 0, len(values))
	for _, value := range values {
		id, err := sourceID(value.Book.ID)
		seconds, secondsErr := parseSeconds(value.ReadTime)
		if err != nil || secondsErr != nil || value.Book.Title == "" {
			return nil, errors.New("source response is invalid")
		}
		records = append(records, RankedRecord{"book:" + id, value.Book.Title, strconv.FormatUint(seconds, 10)})
	}
	return records, nil
}

func parseCategoryRanking(raw json.RawMessage) ([]RankedRecord, error) {
	var values []sourceCategory
	if len(raw) == 0 || string(raw) == "null" {
		return []RankedRecord{}, nil
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("source response is invalid")
	}
	records := make([]RankedRecord, 0, len(values))
	for _, value := range values {
		id, err := sourceID(value.ID)
		seconds, secondsErr := parseSeconds(value.ReadingTime)
		if err != nil || secondsErr != nil || value.Title == "" {
			return nil, errors.New("source response is invalid")
		}
		records = append(records, RankedRecord{"category:" + id, value.Title, strconv.FormatUint(seconds, 10)})
	}
	return records, nil
}

func sourceID(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	switch value := value.(type) {
	case string:
		if value == "" {
			return "", errors.New("empty ID")
		}
		return value, nil
	case json.Number:
		return value.String(), nil
	default:
		return "", errors.New("invalid ID")
	}
}

func parseSeconds(raw json.RawMessage) (uint64, error) {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, err
	}
	return strconv.ParseUint(number.String(), 10, 64)
}
