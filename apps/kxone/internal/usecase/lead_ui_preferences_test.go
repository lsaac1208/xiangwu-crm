package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestLeadUIPreferencesAreAccountScopedAndVersioned(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "preferences.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadListPreferenceRecord{}, &repo.LeadSavedViewRecord{}); err != nil {
		t.Fatal(err)
	}
	uc := NewLeadUIPreferencesUseCase(repo.NewGormLeadUIPreferenceRepository(db))
	ctx := context.Background()
	view, err := uc.CreateView(ctx, "user-a", LeadSavedView{Name: "本周待跟进", Filters: map[string]string{"status": "following"}, SortBy: "next_followup_due_at", SortOrder: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.CreateView(ctx, "user-b", LeadSavedView{Name: "本周待跟进", Filters: map[string]string{}, SortBy: "updated_at", SortOrder: "desc"}); err != nil {
		t.Fatalf("same name must be allowed across accounts: %v", err)
	}
	pref, err := uc.PutPreference(ctx, "user-a", LeadListPreference{Columns: []string{"customer", "owner"}, Density: "compact", DefaultViewID: &view.ID, Version: 0})
	if err != nil || pref.Version != 1 {
		t.Fatalf("preference=%+v err=%v", pref, err)
	}
	if _, err := uc.PutPreference(ctx, "user-a", LeadListPreference{Columns: []string{"customer"}, Density: "compact", Version: 0}); !errors.Is(err, types.ErrLeadPreferenceConflict) {
		t.Fatalf("stale create err=%v", err)
	}
	listed, err := uc.ListViews(ctx, "user-a")
	if err != nil || len(listed) != 1 || listed[0].Name != "本周待跟进" {
		t.Fatalf("views=%+v err=%v", listed, err)
	}
	if err := uc.DeleteView(ctx, "user-a", view.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := uc.GetPreference(ctx, "user-a")
	if err != nil || cleared.DefaultViewID != nil || cleared.Version != 2 {
		t.Fatalf("stale default after delete %+v %v", cleared, err)
	}

}

func TestLeadUIPreferencesRejectUnsafeFiltersAndSort(t *testing.T) {
	db, _ := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "invalid.db"), MaxConns: 1})
	_ = db.AutoMigrate(&repo.LeadListPreferenceRecord{}, &repo.LeadSavedViewRecord{})
	uc := NewLeadUIPreferencesUseCase(repo.NewGormLeadUIPreferenceRepository(db))
	_, err := uc.CreateView(context.Background(), "user", LeadSavedView{Name: "bad", Filters: map[string]string{"sql": "drop"}, SortBy: "updated_at; drop table leads", SortOrder: "desc"})
	if !errors.Is(err, types.ErrInvalidLeadPreference) {
		t.Fatalf("err=%v", err)
	}
}
