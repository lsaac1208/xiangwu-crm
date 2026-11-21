package usecase

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type LeadListPreference struct {
	Columns       []string `json:"columns"`
	Density       string   `json:"density"`
	DefaultViewID *int64   `json:"default_view_id"`
	Version       int      `json:"version"`
}

type LeadSavedView struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Filters   map[string]string `json:"filters"`
	SortBy    string            `json:"sort_by"`
	SortOrder string            `json:"sort_order"`
	Version   int               `json:"version"`
}

type LeadUIPreferencesUseCase struct {
	repository repo.LeadUIPreferenceRepository
}

func NewLeadUIPreferencesUseCase(repository repo.LeadUIPreferenceRepository) *LeadUIPreferencesUseCase {
	return &LeadUIPreferencesUseCase{repository: repository}
}

var allowedLeadColumns = map[string]bool{"customer": true, "stage": true, "source": true, "owner": true, "next_action": true, "created_at": true}
var allowedViewFilters = map[string]bool{"status": true, "keyword": true, "sales_userid": true, "dispatcher_scope": true, "start_date": true, "end_date": true}

func (uc *LeadUIPreferencesUseCase) GetPreference(ctx context.Context, userID string) (*LeadListPreference, error) {
	record, err := uc.repository.GetPreference(ctx, userID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return &LeadListPreference{Columns: []string{"customer", "stage", "source", "owner", "next_action", "created_at"}, Density: "comfortable", Version: 0}, nil
	}
	var columns []string
	if json.Unmarshal([]byte(record.ColumnsJSON), &columns) != nil {
		columns = nil
	}
	return &LeadListPreference{Columns: columns, Density: record.Density, DefaultViewID: record.DefaultViewID, Version: record.Version}, nil
}

func (uc *LeadUIPreferencesUseCase) PutPreference(ctx context.Context, userID string, value LeadListPreference) (*LeadListPreference, error) {
	if value.Density != "comfortable" && value.Density != "compact" {
		return nil, types.ErrInvalidLeadPreference
	}
	seen := map[string]bool{}
	for _, column := range value.Columns {
		if !allowedLeadColumns[column] || seen[column] {
			return nil, types.ErrInvalidLeadPreference
		}
		seen[column] = true
	}
	if len(value.Columns) == 0 {
		return nil, types.ErrInvalidLeadPreference
	}
	if value.DefaultViewID != nil {
		view, err := uc.repository.GetView(ctx, *value.DefaultViewID, userID)
		if err != nil {
			return nil, err
		}
		if view == nil {
			return nil, types.ErrLeadViewNotFound
		}
	}
	encoded, _ := json.Marshal(value.Columns)
	record := &repo.LeadListPreferenceRecord{UserID: userID, ColumnsJSON: string(encoded), Density: value.Density, DefaultViewID: value.DefaultViewID}
	ok, err := uc.repository.UpsertPreference(ctx, record, value.Version)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, types.ErrLeadPreferenceConflict
	}
	value.Version = record.Version
	return &value, nil
}

func (uc *LeadUIPreferencesUseCase) ListViews(ctx context.Context, userID string) ([]LeadSavedView, error) {
	records, err := uc.repository.ListViews(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]LeadSavedView, 0, len(records))
	for _, record := range records {
		result = append(result, savedViewFromRecord(record))
	}
	return result, nil
}

func (uc *LeadUIPreferencesUseCase) CreateView(ctx context.Context, userID string, value LeadSavedView) (*LeadSavedView, error) {
	count, err := uc.repository.CountViews(ctx, userID)
	if err != nil {
		return nil, err
	}
	if count >= 20 {
		return nil, types.ErrLeadViewLimit
	}
	record, err := validateSavedView(userID, 0, value)
	if err != nil {
		return nil, err
	}
	if err := uc.repository.CreateView(ctx, record); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, types.ErrLeadViewNameConflict
		}
		return nil, err
	}
	result := savedViewFromRecord(record)
	return &result, nil
}

func (uc *LeadUIPreferencesUseCase) UpdateView(ctx context.Context, userID string, id int64, value LeadSavedView) (*LeadSavedView, error) {
	if value.Version < 1 {
		return nil, types.ErrInvalidLeadPreference
	}
	current, err := uc.repository.GetView(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, types.ErrLeadViewNotFound
	}
	record, err := validateSavedView(userID, id, value)
	if err != nil {
		return nil, err
	}
	ok, err := uc.repository.UpdateView(ctx, record, value.Version)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, types.ErrLeadViewNameConflict
		}
		return nil, err
	}
	if !ok {
		return nil, types.ErrLeadPreferenceConflict
	}
	result := savedViewFromRecord(record)
	return &result, nil
}

func (uc *LeadUIPreferencesUseCase) DeleteView(ctx context.Context, userID string, id int64) error {
	ok, err := uc.repository.DeleteView(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrLeadViewNotFound
	}
	return nil
}

func validateSavedView(userID string, id int64, value LeadSavedView) (*repo.LeadSavedViewRecord, error) {
	value.Name = strings.TrimSpace(value.Name)
	value.SortBy = strings.TrimSpace(value.SortBy)
	value.SortOrder = strings.ToLower(strings.TrimSpace(value.SortOrder))
	if len([]rune(value.Name)) == 0 || len([]rune(value.Name)) > 64 || !IsAllowedLeadSort(value.SortBy, value.SortOrder) {
		return nil, types.ErrInvalidLeadPreference
	}
	clean := map[string]string{}
	for key, raw := range value.Filters {
		if !allowedViewFilters[key] {
			return nil, types.ErrInvalidLeadPreference
		}
		clean[key] = strings.TrimSpace(raw)
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, types.ErrInvalidLeadPreference
	}
	return &repo.LeadSavedViewRecord{ID: id, UserID: userID, Name: value.Name, FiltersJSON: string(encoded), SortBy: value.SortBy, SortOrder: value.SortOrder, Version: value.Version}, nil
}

func savedViewFromRecord(record *repo.LeadSavedViewRecord) LeadSavedView {
	filters := map[string]string{}
	_ = json.Unmarshal([]byte(record.FiltersJSON), &filters)
	return LeadSavedView{ID: record.ID, Name: record.Name, Filters: filters, SortBy: record.SortBy, SortOrder: record.SortOrder, Version: record.Version}
}
