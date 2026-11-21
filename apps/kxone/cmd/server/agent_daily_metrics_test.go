package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/agent"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

func fixtureWeekStart(now time.Time) time.Time {
	local := now.In(types.BusinessLocation)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, types.BusinessLocation)
	return today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
}

// Shift the fixed gold fixture as one timeline so its reference week remains
// last week; this changes only the in-memory data used by this test.
func rebaseDailyTemplateFixture(t testing.TB, raw []byte, now time.Time) []byte {
	t.Helper()
	var fixture liveFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	reference, err := time.Parse(time.RFC3339, fixture.ReferenceNow)
	if err != nil {
		t.Fatal(err)
	}
	days := int(fixtureWeekStart(now).AddDate(0, 0, -7).Sub(fixtureWeekStart(reference)).Hours() / 24)
	for _, rows := range fixture.Tables {
		for _, row := range rows {
			for key, value := range row {
				text, ok := value.(string)
				if !ok || !strings.HasSuffix(key, "_at") {
					continue
				}
				stamp, err := time.Parse(time.RFC3339, text)
				if err != nil {
					t.Fatalf("invalid fixture time %s: %v", key, err)
				}
				row[key] = stamp.AddDate(0, 0, days).Format(time.RFC3339)
			}
		}
	}
	fixture.ReferenceNow = reference.AddDate(0, 0, days).Format(time.RFC3339)
	shifted, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return shifted
}

type dailyFixtureCounts struct {
	total   int
	sources map[string]int
	owners  map[string]int
}

func countDailyFixtureLeads(t testing.TB, fixture liveFixture, start, end time.Time) dailyFixtureCounts {
	t.Helper()
	counts := dailyFixtureCounts{sources: map[string]int{}, owners: map[string]int{}}
	for _, lead := range fixture.Tables["leads"] {
		stamp, err := time.Parse(time.RFC3339, lead["created_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if !stamp.Before(start) && stamp.Before(end) {
			source, _ := lead["source_channel"].(string)
			owner, _ := lead["current_owner_user_id"].(string)
			counts.total++
			counts.sources[source]++
			counts.owners[owner]++
		}
	}
	return counts
}

func TestDailyTemplateFixtureDateConditions(t *testing.T) {
	baseRaw, _ := readFixtureFile(t, "../../docs/Agent-Evaluation-Fixture.json")
	dailyRaw, _ := readFixtureFile(t, "../../docs/Agent-Daily-Fixture.json")
	for _, tc := range []struct {
		date          time.Time
		lastWeekTotal int
		monthSource   int
	}{
		{time.Date(2026, time.October, 14, 10, 0, 0, 0, types.BusinessLocation), 5, 4},
		{time.Date(2026, time.November, 5, 10, 0, 0, 0, types.BusinessLocation), 6, 2},
		{time.Date(2027, time.April, 7, 10, 0, 0, 0, types.BusinessLocation), 6, 3},
	} {
		fixture, _, err := buildLiveDailyFixture(rebaseDailyTemplateFixture(t, baseRaw, tc.date), dailyRaw, tc.date)
		if err != nil {
			t.Fatal(err)
		}
		lastWeek := fixtureWeekStart(tc.date).AddDate(0, 0, -7)
		month := time.Date(tc.date.Year(), tc.date.Month(), 1, 0, 0, 0, 0, types.BusinessLocation)
		if got := countDailyFixtureLeads(t, fixture, lastWeek, lastWeek.AddDate(0, 0, 7)).total; got != tc.lastWeekTotal {
			t.Errorf("date=%s last-week leads=%d want=%d", tc.date.Format("2006-01-02"), got, tc.lastWeekTotal)
		}
		if got := countDailyFixtureLeads(t, fixture, month, month.AddDate(0, 1, 0)).sources["好学校"]; got != tc.monthSource {
			t.Errorf("date=%s month source leads=%d want=%d", tc.date.Format("2006-01-02"), got, tc.monthSource)
		}
	}
}

// Everyday questions on the daily dataset: the server parser picks the
// metric and the server template answers, with no model involved.
func TestAgentDailyQuestionsAnswerFromServerTemplates(t *testing.T) {
	now := time.Now()
	local := now.In(types.BusinessLocation)
	if local.Weekday() == time.Monday || local.Weekday() == time.Tuesday || local.Day() <= 3 {
		t.Skip("daily fixture requires Wednesday–Sunday after the 3rd")
	}
	baseRaw, _ := readFixtureFile(t, "../../docs/Agent-Evaluation-Fixture.json")
	dailyRaw, _ := readFixtureFile(t, "../../docs/Agent-Daily-Fixture.json")
	fixture, _, err := buildLiveDailyFixture(rebaseDailyTemplateFixture(t, baseRaw, now), dailyRaw, now)
	if err != nil {
		t.Fatal(err)
	}
	var db *gorm.DB
	router := newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	admin := issueDevToken(t, router, "daily-admin", "admin")
	created := doJSON(t, router, http.MethodPost, "/api/agent/conversations", admin, map[string]any{})
	var conversation struct{ ID string }
	_ = json.Unmarshal(created.Body.Bytes(), &conversation)
	ask := func(question string, previous []agent.MetricSelection) (string, []agent.MetricSelection) {
		sels := agent.HeuristicSelections(question, now, previous)
		if len(sels) != 1 {
			t.Fatalf("%s: selections %v", question, sels)
		}
		response := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/metric-runs", admin, map[string]any{"metric_id": sels[0].MetricID, "params": sels[0].Params, "request_id": "daily-" + uuid.NewString()})
		var run struct{ ID string }
		_ = json.Unmarshal(response.Body.Bytes(), &run)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var body struct {
				Run      struct{ Status string }
				Messages []struct{ Role, Content string }
			}
			_ = json.Unmarshal(doJSON(t, router, http.MethodGet, "/api/agent/runs/"+run.ID, admin, nil).Body.Bytes(), &body)
			if body.Run.Status == "completed" {
				for _, m := range body.Messages {
					if m.Role == "assistant" {
						return m.Content, sels
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s did not complete", question)
		return "", nil
	}
	expect := func(question, content string, fragments ...string) {
		first := strings.SplitN(content, "\n\n", 2)[0]
		for _, f := range fragments {
			if !strings.Contains(first, f) {
				t.Fatalf("%s: answer %q lacks %q", question, first, f)
			}
		}
	}
	lastWeek := fixtureWeekStart(now).AddDate(0, 0, -7)
	lastWeekCounts := countDailyFixtureLeads(t, fixture, lastWeek, lastWeek.AddDate(0, 0, 7))
	got, prev := ask("上周共有几条线索进来", nil)
	expect("上周", got, "上周（", fmt.Sprintf("一共进来 %d 条新线索", lastWeekCounts.total))
	got, prev = ask("那哪个来源最多", prev)
	expect("追问来源", got, fmt.Sprintf("「好学校」最多，共 %d 条", lastWeekCounts.sources["好学校"]))
	got, _ = ask("按负责人拆开", prev)
	expect("追问负责人", got, fmt.Sprintf("员工乙 %d 条", lastWeekCounts.owners["sales-a"]), fmt.Sprintf("小宇 %d 条", lastWeekCounts.owners["sales-b"]), fmt.Sprintf("公海 %d 条", lastWeekCounts.owners[""]))
	got, _ = ask("本月哪个来源最多", nil)
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, types.BusinessLocation)
	expect("本月来源", got, "本月", fmt.Sprintf("「好学校」最多，共 %d 条", countDailyFixtureLeads(t, fixture, month, month.AddDate(0, 1, 0)).sources["好学校"]))
	got, _ = ask("昨天新增多少", nil)
	expect("昨天", got, "一共进来 2 条新线索")
	got, _ = ask("谁的待办最多", nil)
	expect("待办", got, "「员工乙」最多，共 3 项")
	got, _ = ask("这个月成交了几单", nil)
	expect("成交", got, "本月成交了 2 单")
}
