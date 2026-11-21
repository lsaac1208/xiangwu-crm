package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

const liveDailyDataset = "daily-v1"

var liveRelativeTime = regexp.MustCompile(`^@rel:(today|yesterday|last_monday|month_first)\+(\d+) (\d{2}):(\d{2})$`)

// resolveLiveRelativeTime turns "@rel:anchor+N HH:MM" into a Shanghai RFC3339
// timestamp relative to now, so everyday phrases (上周/本月/昨天) have data.
func resolveLiveRelativeTime(value string, now time.Time) (string, error) {
	m := liveRelativeTime.FindStringSubmatch(value)
	if m == nil {
		return "", fmt.Errorf("invalid relative time %q", value)
	}
	local := now.In(types.BusinessLocation)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, types.BusinessLocation)
	anchor := today
	switch m[1] {
	case "yesterday":
		anchor = today.AddDate(0, 0, -1)
	case "last_monday":
		anchor = today.AddDate(0, 0, -((int(today.Weekday())+6)%7)-7)
	case "month_first":
		anchor = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, types.BusinessLocation)
	}
	days, _ := strconv.Atoi(m[2])
	hour, _ := strconv.Atoi(m[3])
	minute, _ := strconv.Atoi(m[4])
	return anchor.AddDate(0, 0, days).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute).Format(time.RFC3339), nil
}

// The daily dataset extends gold-v1 with rows dated relative to the run day.
func buildLiveDailyFixture(baseRaw, dailyRaw []byte, now time.Time) (liveFixture, string, error) {
	var base liveFixture
	if err := json.Unmarshal(baseRaw, &base); err != nil || base.Version != "agent-evaluation-fixture-v1" || len(base.Tables["leads"]) != 7 {
		return liveFixture{}, "", fmt.Errorf("unrecognized base fixture")
	}
	var daily struct {
		Version   string                      `json:"version"`
		Dataset   string                      `json:"dataset"`
		Tables    map[string][]map[string]any `json:"tables"`
		Forbidden []any                       `json:"forbidden_provider_payload_fragments"`
	}
	if err := json.Unmarshal(dailyRaw, &daily); err != nil || daily.Version != "agent-daily-fixture-v1" || daily.Dataset != liveDailyDataset {
		return liveFixture{}, "", fmt.Errorf("unrecognized daily fixture")
	}
	local := now.In(types.BusinessLocation)
	if wd := local.Weekday(); wd == time.Monday || wd == time.Tuesday || local.Day() <= 3 {
		return liveFixture{}, "", fmt.Errorf("daily fixture must run Wednesday–Sunday after the 3rd so periods do not overlap")
	}
	for name, rows := range daily.Tables {
		for _, row := range rows {
			for key, value := range row {
				if text, ok := value.(string); ok && len(text) > 5 && text[:5] == "@rel:" {
					resolved, err := resolveLiveRelativeTime(text, now)
					if err != nil {
						return liveFixture{}, "", err
					}
					row[key] = resolved
				}
			}
			base.Tables[name] = append(base.Tables[name], row)
		}
	}
	forbidden, _ := base.IndependentExpected["forbidden_provider_payload_fragments"].([]any)
	base.IndependentExpected["forbidden_provider_payload_fragments"] = append(append([]any{}, forbidden...), daily.Forbidden...)
	base.Dataset = liveDailyDataset
	sum := sha256.Sum256(append(append([]byte{}, baseRaw...), dailyRaw...))
	base.ContentHash = hex.EncodeToString(sum[:])
	return base, base.ContentHash, nil
}

func loadLiveDailyFixture(t *testing.T) (liveFixture, string) {
	t.Helper()
	baseRaw, err := os.ReadFile("../../docs/Agent-Evaluation-Fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	dailyRaw, err := os.ReadFile("../../docs/Agent-Daily-Fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture, hash, err := buildLiveDailyFixture(baseRaw, dailyRaw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return fixture, hash
}

func TestLiveDailyFixtureResolvesRelativeDates(t *testing.T) {
	baseRaw, _ := os.ReadFile("../../docs/Agent-Evaluation-Fixture.json")
	dailyRaw, _ := os.ReadFile("../../docs/Agent-Daily-Fixture.json")
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, types.BusinessLocation)
	fixture, _, err := buildLiveDailyFixture(baseRaw, dailyRaw, now)
	if err != nil {
		t.Fatal(err)
	}
	leads := fixture.Tables["leads"]
	if len(leads) != 13 || leads[7]["created_at"] != "2026-10-06T10:00:00+08:00" || leads[10]["converted_at"] != "2026-10-13T15:00:00+08:00" || leads[12]["created_at"] != "2026-10-01T10:00:00+08:00" {
		t.Fatalf("resolved %v %v %v", leads[7]["created_at"], leads[10]["converted_at"], leads[12]["created_at"])
	}
	if _, _, err := buildLiveDailyFixture(baseRaw, dailyRaw, time.Date(2026, 10, 12, 10, 0, 0, 0, types.BusinessLocation)); err == nil {
		t.Fatal("Monday run accepted")
	}
}

func readFixtureFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw, nil
}
