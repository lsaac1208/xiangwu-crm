package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestSnapshotSearchInclusiveTimeBoundsAndOffset(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	dayStart := time.Date(2026, 10, 8, 0, 0, 0, 0, shanghai)
	noon := dayStart.Add(12 * time.Hour)
	nextMidnight := dayStart.AddDate(0, 0, 1)
	// Adjacent nanoseconds distinguish inclusive precise bounds from half-open
	// bounds. A date-only upper bound includes the day end but not next midnight.
	timestamps := []time.Time{dayStart, nextMidnight.Add(-time.Nanosecond), nextMidnight, noon, noon.Add(time.Nanosecond), noon.Add(-time.Nanosecond)}
	for i, at := range timestamps {
		row := repo.LeadFollowupModel{ID: int64(i + 1), LeadID: 1, SalesUserID: "boundary-worker", BusinessStage: "invitation", ActorRole: "inviter", FollowupResult: "connected", Remark: "boundary fixture", IsValid: true, CreatedAt: at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, test := range []struct {
		name, from, to string
		offset, limit  int
		want           []int64
		total          int64
		truncated      bool
	}{
		{"date includes final nanosecond", "2026-10-08", "2026-10-08", 0, 20, []int64{2, 5, 4, 6, 1}, 5, false},
		{"following date includes midnight", "2026-10-09", "2026-10-09", 0, 20, []int64{3}, 1, false},
		{"precise UTC upper bound inclusive", "2026-10-08", "2026-10-08T04:00:00Z", 0, 20, []int64{4, 6, 1}, 3, false},
		{"same precise bounds include instant", "2026-10-08T12:00:00+08:00", "2026-10-08T12:00:00+08:00", 0, 20, []int64{4}, 1, false},
		{"explicit next midnight includes it", "2026-10-08T23:59:59.999999999+08:00", "2026-10-09T00:00:00+08:00", 0, 20, []int64{3, 2}, 2, false},
		{"page one", "2026-10-08", "2026-10-08", 0, 2, []int64{2, 5}, 5, true},
		{"page two", "2026-10-08", "2026-10-08", 2, 2, []int64{4, 6}, 5, true},
		{"last page", "2026-10-08", "2026-10-08", 4, 2, []int64{1}, 5, false},
		{"past final page", "2026-10-08", "2026-10-08", 5, 2, []int64{}, 5, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := snapshot.Search(t.Context(), SearchRequest{ActorID: "boundary-worker", Stage: "invitation", From: test.from, To: test.to, Offset: test.offset, Limit: test.limit})
			if err != nil {
				t.Fatal(err)
			}
			ids := []int64{}
			for _, row := range result.Rows {
				if row[0] != "lead_followups" {
					t.Fatalf("search matched unrelated record: %v", row[0])
				}
				id, ok := row[1].(int64)
				if !ok {
					t.Fatalf("unexpected record ID type %T", row[1])
				}
				ids = append(ids, id)
			}
			if fmt.Sprint(ids) != fmt.Sprint(test.want) || result.TotalRows != test.total || result.ReturnedRows != len(test.want) || result.Offset != test.offset || result.Truncated != test.truncated {
				t.Fatalf("time/page boundary got ids=%v total=%d returned=%d offset=%d truncated=%v; want ids=%v total=%d offset=%d truncated=%v", ids, result.TotalRows, result.ReturnedRows, result.Offset, result.Truncated, test.want, test.total, test.offset, test.truncated)
			}
		})
	}
	page, err := snapshot.Search(t.Context(), SearchRequest{ActorID: "boundary-worker", Stage: "invitation", From: "2026-10-08", To: "2026-10-08", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	full, err := snapshot.MaterializeResult(t.Context(), page, 100)
	if err != nil {
		t.Fatal(err)
	}
	if full.Offset != 0 || full.TotalRows != 5 || full.ReturnedRows != 5 || full.Truncated {
		t.Fatalf("complete materialization must restart at offset zero: offset=%d total=%d returned=%d truncated=%v", full.Offset, full.TotalRows, full.ReturnedRows, full.Truncated)
	}
	// Historical result API pagination is a fresh page over the complete saved
	// result, distinct from the original model-read page's offset.
	savedPage := resultPage(*full, 2, 2)
	if savedPage.Offset != 2 || savedPage.ReturnedRows != 2 || !savedPage.Truncated || fmt.Sprint([]any{savedPage.Rows[0][1], savedPage.Rows[1][1]}) != "[4 6]" {
		t.Fatalf("saved result page lost its own offset or range: %+v", savedPage)
	}
}
