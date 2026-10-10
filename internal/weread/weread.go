// Package weread provides shared, typed source facts to Dataset Metrics.
package weread

import (
	"context"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
)

type DailyRecord struct {
	Date    string
	Seconds uint64
}

type ReadingRanking struct {
	ID         string
	Label      string
	Seconds    uint64
	Author     string
	ReaderLink string
}

type NoteCount struct {
	BookID          string
	Label           string
	Highlights      uint64
	ThoughtsReviews uint64
	Bookmarks       uint64
}

func (count NoteCount) Total() (uint64, bool) {
	if ^uint64(0)-count.Highlights < count.ThoughtsReviews {
		return 0, false
	}
	total := count.Highlights + count.ThoughtsReviews
	if ^uint64(0)-total < count.Bookmarks {
		return 0, false
	}
	return total + count.Bookmarks, true
}

// Gateway is the shared source adapter. Metric packages narrow this interface
// to the operations they consume.
type Gateway interface {
	DailyRecords(context.Context, []v2.Range) ([]DailyRecord, error)
	PeriodRanking(context.Context, v2.Range, string) ([]ReadingRanking, error)
	NotebookCounts(context.Context) ([]NoteCount, error)
}

// FakeGateway is deterministic test infrastructure for command and Metric tests.
type FakeGateway struct {
	Records        []DailyRecord
	RankingRecords []ReadingRanking
	Notes          []NoteCount
	Err            error
}

func (g FakeGateway) DailyRecords(_ context.Context, _ []v2.Range) ([]DailyRecord, error) {
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]DailyRecord(nil), g.Records...), nil
}

func (g FakeGateway) PeriodRanking(_ context.Context, _ v2.Range, _ string) ([]ReadingRanking, error) {
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]ReadingRanking(nil), g.RankingRecords...), nil
}

func (g FakeGateway) NotebookCounts(_ context.Context) ([]NoteCount, error) {
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]NoteCount(nil), g.Notes...), nil
}
