package weread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
)

func TestParseRankingsReturnTypedSeconds(t *testing.T) {
	books, err := parseBookRanking(json.RawMessage(`[{"book":{"bookId":42,"title":"Book","author":"Author","readerLink":"https://weread.qq.com/web/reader/42"},"readTime":120}]`))
	if err != nil || len(books) != 1 || books[0] != (ReadingRanking{ID: "book:42", Label: "Book", Seconds: 120, Author: "Author", ReaderLink: "https://weread.qq.com/web/reader/42"}) {
		t.Fatalf("books = %#v, err = %v", books, err)
	}
	categories, err := parseCategoryRanking(json.RawMessage(`[{"categoryId":7,"categoryTitle":"History","readingTime":180}]`))
	if err != nil || len(categories) != 1 || categories[0] != (ReadingRanking{ID: "category:7", Label: "History", Seconds: 180}) {
		t.Fatalf("categories = %#v, err = %v", categories, err)
	}
}

func TestSourceFailureMessagesAreActionableAndCredentialSafe(t *testing.T) {
	tests := []struct {
		kind sourceFailureKind
		want string
	}{
		{sourceFailureCredential, "WEREAD_API_KEY is not configured"},
		{sourceFailureAuthentication, "Check that WEREAD_API_KEY is current"},
		{sourceFailureNetwork, "network, proxy, and firewall"},
		{sourceFailureHTTP, "HTTP service error"},
		{sourceFailureUpgrade, "newer Skill version"},
		{sourceFailureResponse, "unsupported or malformed response"},
	}
	for _, test := range tests {
		en, zh := SourceFailureMessage(newSourceFailure(test.kind))
		if !strings.Contains(en, test.want) || zh == "" || strings.Contains(en+zh, "test-key") {
			t.Fatalf("kind %q: en=%q zh=%q", test.kind, en, zh)
		}
	}
}

func TestGatewayClassifiesAuthorizationAndServiceStatus(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   sourceFailureKind
	}{
		{http.StatusUnauthorized, sourceFailureAuthentication},
		{http.StatusForbidden, sourceFailureAuthentication},
		{http.StatusBadGateway, sourceFailureHTTP},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(test.status)
		}))
		gateway := HTTPGateway{client: server.Client(), apiKey: "test-key", endpoint: gatewayURL}
		gateway.client.Transport = rewriteTransport{target: server.URL}
		_, err := gateway.fetchGateway(context.Background(), map[string]any{"api_name": "/test"})
		server.Close()
		if sourceFailureKindOf(err) != test.kind {
			t.Fatalf("status %d: error = %v", test.status, err)
		}
	}
}

func TestDailyRecordsReturnTypedSeconds(t *testing.T) {
	gateway := testGateway(t, func(body map[string]any) string {
		if body["skill_version"] != "1.0.5" {
			t.Fatalf("skill_version = %#v", body["skill_version"])
		}
		if body["mode"] == "annually" {
			return `{"errcode":0,"totalReadTime":60,"dailyReadTimes":{"2026-09-17":60}}`
		}
		return `{"errcode":0,"totalReadTime":60,"readTimes":{"2026-09-17":60}}`
	})
	records, err := gateway.DailyRecords(context.Background(), []v2.Range{{StartDate: "2026-09-17", EndDate: "2026-09-17"}})
	if err != nil || len(records) != 1 || records[0].Seconds != 60 {
		t.Fatalf("records = %#v, err = %v", records, err)
	}
}

func TestNotebookCountsDeduplicatesBookIDs(t *testing.T) {
	requests := 0
	gateway := testGateway(t, func(body map[string]any) string {
		requests++
		if requests == 1 {
			return `{"errcode":0,"hasMore":1,"books":[{"bookId":42,"book":{"title":"Book"},"reviewCount":1,"noteCount":2,"bookmarkCount":3,"sort":10}]}`
		}
		return `{"errcode":0,"hasMore":0,"books":[{"bookId":42,"book":{"title":"Book"},"reviewCount":1,"noteCount":2,"bookmarkCount":3,"sort":9}]}`
	})
	counts, err := gateway.NotebookCounts(context.Background())
	if err != nil || len(counts) != 1 || counts[0] != (NoteCount{BookID: "42", Label: "Book", ThoughtsReviews: 1, Highlights: 2, Bookmarks: 3}) {
		t.Fatalf("counts = %#v, err = %v", counts, err)
	}
}

func TestNotebookCountsRejectsRepeatedCursor(t *testing.T) {
	gateway := testGateway(t, func(map[string]any) string {
		return `{"errcode":0,"hasMore":1,"books":[{"bookId":42,"book":{"title":"Book"},"reviewCount":1,"noteCount":2,"bookmarkCount":3,"sort":10}]}`
	})
	if _, err := gateway.NotebookCounts(context.Background()); err == nil {
		t.Fatal("accepted repeated cursor")
	}
}

func testGateway(t *testing.T, script func(map[string]any) string) HTTPGateway {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(script(body)))
	}))
	t.Cleanup(server.Close)
	return HTTPGateway{client: &http.Client{Transport: rewriteTransport{target: server.URL}}, apiKey: "test-key", endpoint: gatewayURL}
}

type rewriteTransport struct{ target string }

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(transport.target, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}
