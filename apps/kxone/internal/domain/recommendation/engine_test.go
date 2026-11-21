package recommendation

import (
	"testing"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestEngineRecommendSortsByScoreAndSkipsInvalidProfiles(t *testing.T) {
	engine := NewEngine()
	lead := &types.Lead{
		City:        "杭州",
		ProductName: "课程A",
	}

	scores := engine.Recommend(&RecommendationRequest{
		Lead: lead,
		SalesProfiles: []*SalesProfile{
			nil,
			{UserID: ""},
			{
				UserID:            "sales-low",
				Name:              "低分销售",
				PreferredCities:   []string{"上海"},
				PreferredProducts: []string{"课程B"},
				CurrentLeadsCount: 9,
				AvgClaimTime:      1800,
				ConversionRate:    0.05,
				TotalClaimed:      3,
			},
			{
				UserID:            "sales-high",
				Name:              "高分销售",
				PreferredCities:   []string{"杭州"},
				PreferredProducts: []string{"课程A"},
				CurrentLeadsCount: 0,
				AvgClaimTime:      120,
				ConversionRate:    0.4,
				TotalClaimed:      80,
			},
		},
	})

	if len(scores) != 2 {
		t.Fatalf("scores len = %d, want 2", len(scores))
	}
	if scores[0].SalesUserID != "sales-high" {
		t.Fatalf("top sales = %s, want sales-high", scores[0].SalesUserID)
	}
	if scores[0].TotalScore <= scores[1].TotalScore {
		t.Fatalf("scores not sorted descending: %+v", scores)
	}
	if scores[0].Reason == "" {
		t.Fatal("reason is empty")
	}
}

func TestEngineRecommendHandlesNilRequest(t *testing.T) {
	engine := NewEngine()

	if scores := engine.Recommend(nil); len(scores) != 0 {
		t.Fatalf("nil request scores len = %d, want 0", len(scores))
	}
	if scores := engine.Recommend(&RecommendationRequest{}); len(scores) != 0 {
		t.Fatalf("empty request scores len = %d, want 0", len(scores))
	}
}
