package usecase

import (
	"context"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type managerGlobalListRepository struct {
	*MockLeadRepository
	allCalls     int
	managerCalls int
}

func (r *managerGlobalListRepository) ListAllFiltered(_ context.Context, _ repo.LeadListFilter, _, _ int) ([]*types.Lead, int, error) {
	r.allCalls++
	return []*types.Lead{{ID: 42, CustomerMobile: "13912345678"}}, 1, nil
}

func (r *managerGlobalListRepository) ListByManagerFiltered(_ context.Context, _ string, _ repo.LeadListFilter, _, _ int) ([]*types.Lead, int, error) {
	r.managerCalls++
	return nil, 0, nil
}

func TestListLeadsManagerUsesGlobalReadScope(t *testing.T) {
	leads := &managerGlobalListRepository{MockLeadRepository: NewMockLeadRepository()}
	result, err := NewListLeadsUseCase(leads).Execute(context.Background(), &ListLeadsRequest{
		CurrentUser: "manager-1",
		Role:        string(types.UserRoleManager),
		Page:        1,
		PageSize:    20,
	})
	if err != nil {
		t.Fatalf("list manager leads: %v", err)
	}
	if leads.allCalls != 1 || leads.managerCalls != 0 {
		t.Fatalf("repository calls all=%d manager=%d, want 1/0", leads.allCalls, leads.managerCalls)
	}
	if result.Total != 1 || len(result.Leads) != 1 || result.Leads[0].ID != 42 {
		t.Fatalf("result = %+v, want global lead", result)
	}
	if result.Leads[0].CustomerMobile != "13912345678" {
		t.Fatalf("manager mobile = %q, want full mobile", result.Leads[0].CustomerMobile)
	}
}
