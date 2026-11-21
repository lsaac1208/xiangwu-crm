package types

import "testing"

func TestStarRatingAndLegacyMapping(t *testing.T) {
	for rating := 1; rating <= 5; rating++ {
		if !IsValidStarRating(rating) {
			t.Fatalf("rating %d should be valid", rating)
		}
	}
	if IsValidStarRating(0) || IsValidStarRating(6) {
		t.Fatal("ratings outside 1-5 must be invalid")
	}
	if LegacyIntentionStarRating("high") != 5 || LegacyIntentionStarRating("medium") != 3 || LegacyIntentionStarRating("low") != 1 {
		t.Fatal("legacy intention mapping is incorrect")
	}
}
