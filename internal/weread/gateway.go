package weread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
)

const (
	gatewayURL         = "https://i.weread.qq.com/api/agent/gateway"
	skillVersion       = "1.0.5"
	maxResponseBytes   = 1 << 20
	maxNotebookPages   = 100
	maxNotebookRecords = 10_000
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
	sourceFailureCredential     sourceFailureKind = "credential"
	sourceFailureAuthentication sourceFailureKind = "authentication"
	sourceFailureNetwork        sourceFailureKind = "network"
	sourceFailureHTTP           sourceFailureKind = "http"
	sourceFailurePeriod         sourceFailureKind = "period"
	sourceFailureRequest        sourceFailureKind = "request"
	sourceFailureUpgrade        sourceFailureKind = "upgrade"
	sourceFailureResponse       sourceFailureKind = "response"
	sourceFailureRanking        sourceFailureKind = "ranking"
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

// SourceFailureMessage returns actionable diagnostics without exposing
// credentials, authorization headers, response bodies, or private endpoints.
func SourceFailureMessage(err error) (string, string) {
	switch sourceFailureKindOf(err) {
	case sourceFailureCredential:
		return "WEREAD_API_KEY is not configured. Export it in the shell that starts ccuv.", "未配置 WEREAD_API_KEY。请在启动 ccuv 的 Shell 中导出该环境变量。"
	case sourceFailureAuthentication:
		return "The WeRead gateway rejected the request. Check that WEREAD_API_KEY is current and authorized for weread-skills.", "微信读书网关拒绝了请求。请检查 WEREAD_API_KEY 是否有效，并已获得 weread-skills 授权。"
	case sourceFailureNetwork:
		return "The WeRead gateway could not be reached. Check the network, proxy, and firewall, then retry.", "无法连接微信读书网关。请检查网络、代理和防火墙后重试。"
	case sourceFailureHTTP:
		return "The WeRead gateway returned an HTTP service error. Retry later.", "微信读书网关返回 HTTP 服务错误，请稍后重试。"
	case sourceFailureUpgrade:
		return "The WeRead gateway requires a newer Skill version. Update the WeRead Dataset.", "微信读书网关要求更新的 Skill 版本。请更新 WeRead Dataset。"
	case sourceFailureResponse:
		return "The WeRead gateway returned an unsupported or malformed response. Update the WeRead Dataset or retry later.", "微信读书网关返回了不支持或格式异常的响应。请更新 WeRead Dataset 或稍后重试。"
	case sourceFailurePeriod:
		return "The selected WeRead period cannot be converted to a source request.", "所选微信读书周期无法转换为数据源请求。"
	case sourceFailureRanking:
		return "The WeRead ranking response does not match the selected grouping.", "微信读书排行响应与所选分组不匹配。"
	default:
		return "The WeRead request could not be completed.", "微信读书请求无法完成。"
	}
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
		return HTTPGateway{}, newSourceFailure(sourceFailureCredential)
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

func (g HTTPGateway) PeriodRanking(ctx context.Context, interval v2.Range, groupBy string) ([]ReadingRanking, error) {
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
	var records []ReadingRanking
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

func (g HTTPGateway) NotebookCounts(ctx context.Context) ([]NoteCount, error) {
	if g.client == nil || g.endpoint != gatewayURL || strings.TrimSpace(g.apiKey) == "" {
		return nil, newSourceFailure(sourceFailureRequest)
	}
	records := make([]NoteCount, 0)
	seen := make(map[string]struct{})
	var lastSort *int64
	for page := 0; page < maxNotebookPages; page++ {
		body := map[string]any{"api_name": "/user/notebooks", "count": 100, "skill_version": skillVersion}
		if lastSort != nil {
			body["lastSort"] = *lastSort
		}
		payload, err := g.fetchGateway(ctx, body)
		if err != nil {
			return nil, err
		}
		var response struct {
			ErrCode     *int            `json:"errcode"`
			UpgradeInfo json.RawMessage `json:"upgrade_info"`
			HasMore     int             `json:"hasMore"`
			Books       []struct {
				BookID json.RawMessage `json:"bookId"`
				Book   struct {
					Title string `json:"title"`
				} `json:"book"`
				ReviewCount   uint64 `json:"reviewCount"`
				NoteCount     uint64 `json:"noteCount"`
				BookmarkCount uint64 `json:"bookmarkCount"`
				Sort          int64  `json:"sort"`
			} `json:"books"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, newSourceFailure(sourceFailureResponse)
		}
		if len(response.UpgradeInfo) > 0 && string(response.UpgradeInfo) != "null" {
			return nil, newSourceFailure(sourceFailureUpgrade)
		}
		if response.ErrCode != nil && *response.ErrCode != 0 {
			return nil, newSourceFailure(sourceFailureAuthentication)
		}
		for _, book := range response.Books {
			id, err := sourceID(book.BookID)
			if err != nil {
				return nil, newSourceFailure(sourceFailureResponse)
			}
			if id == "" || book.Book.Title == "" || ^uint64(0)-book.ReviewCount < book.NoteCount || ^uint64(0)-book.ReviewCount-book.NoteCount < book.BookmarkCount {
				return nil, newSourceFailure(sourceFailureResponse)
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			if len(records) >= maxNotebookRecords {
				return nil, newSourceFailure(sourceFailureResponse)
			}
			seen[id] = struct{}{}
			records = append(records, NoteCount{BookID: id, Label: book.Book.Title, Highlights: book.NoteCount, ThoughtsReviews: book.ReviewCount, Bookmarks: book.BookmarkCount})
		}
		if response.HasMore == 0 {
			return records, nil
		}
		if len(response.Books) == 0 {
			return nil, newSourceFailure(sourceFailureResponse)
		}
		next := response.Books[len(response.Books)-1].Sort
		if lastSort != nil && next == *lastSort {
			return nil, newSourceFailure(sourceFailureResponse)
		}
		lastSort = &next
	}
	return nil, newSourceFailure(sourceFailureResponse)
}

func (g HTTPGateway) DailyRecords(ctx context.Context, intervals []v2.Range) ([]DailyRecord, error) {
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
		records = append(records, DailyRecord{Date: key, Seconds: seconds})
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
	return g.fetchGateway(ctx, map[string]any{
		"api_name": "/readdata/detail", "mode": mode, "baseTime": baseTime, "skill_version": skillVersion,
	})

}

func (g HTTPGateway) fetchGateway(ctx context.Context, values map[string]any) ([]byte, error) {
	body, err := json.Marshal(values)
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
		return nil, newSourceFailure(sourceFailureNetwork)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, newSourceFailure(sourceFailureAuthentication)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, newSourceFailure(sourceFailureHTTP)
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
		return detailResponse{}, newSourceFailure(sourceFailureAuthentication)
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

func requestedDates(intervals []v2.Range) ([]time.Time, error) {
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

func sourcePeriod(interval v2.Range) (string, int64, error) {
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
		ID         json.RawMessage `json:"bookId"`
		Title      string          `json:"title"`
		Author     string          `json:"author"`
		ReaderLink string          `json:"readerLink"`
	} `json:"book"`
	ReadTime json.RawMessage `json:"readTime"`
}

type sourceCategory struct {
	ID          json.RawMessage `json:"categoryId"`
	Title       string          `json:"categoryTitle"`
	ReadingTime json.RawMessage `json:"readingTime"`
}

func parseBookRanking(raw json.RawMessage) ([]ReadingRanking, error) {
	var values []sourceBook
	if len(raw) == 0 || string(raw) == "null" {
		return []ReadingRanking{}, nil
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("source response is invalid")
	}
	records := make([]ReadingRanking, 0, len(values))
	for _, value := range values {
		id, err := sourceID(value.Book.ID)
		seconds, secondsErr := parseSeconds(value.ReadTime)
		if err != nil || secondsErr != nil || value.Book.Title == "" {
			return nil, errors.New("source response is invalid")
		}
		records = append(records, ReadingRanking{ID: "book:" + id, Label: value.Book.Title, Seconds: seconds, Author: value.Book.Author, ReaderLink: validReaderLink(value.Book.ReaderLink)})
	}
	return records, nil
}

func parseCategoryRanking(raw json.RawMessage) ([]ReadingRanking, error) {
	var values []sourceCategory
	if len(raw) == 0 || string(raw) == "null" {
		return []ReadingRanking{}, nil
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("source response is invalid")
	}
	records := make([]ReadingRanking, 0, len(values))
	for _, value := range values {
		id, err := sourceID(value.ID)
		seconds, secondsErr := parseSeconds(value.ReadingTime)
		if err != nil || secondsErr != nil || value.Title == "" {
			return nil, errors.New("source response is invalid")
		}
		records = append(records, ReadingRanking{ID: "category:" + id, Label: value.Title, Seconds: seconds})
	}
	return records, nil
}

func validReaderLink(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	return value
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
