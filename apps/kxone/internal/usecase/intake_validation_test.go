package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestExternalIntakeEnforcesProfileConstraints(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	uc := NewCreateLeadUseCase(leads, nil)
	for _, tc := range []struct {
		name  string
		patch func(*CreateLeadRequest)
	}{
		{"long name", func(r *CreateLeadRequest) { r.CustomerName = strings.Repeat("名", 65) }},
		{"invalid contact", func(r *CreateLeadRequest) { r.CustomerMobile = "enc:v1:abc" }},
		{"long campaign", func(r *CreateLeadRequest) { r.SourceCampaign = strings.Repeat("x", 513) }},
		{"long source", func(r *CreateLeadRequest) { r.SourceChannel = strings.Repeat("x", 129) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &CreateLeadRequest{CustomerName: "客户", CustomerMobile: "13912345678", DispatchManagerUserID: "manager-1"}
			tc.patch(r)
			if _, err := uc.Execute(context.Background(), r); !errors.Is(err, types.ErrInvalidLeadProfile) {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
	items, n, err := leads.ListAllFiltered(context.Background(), repo.LeadListFilter{}, 0, 20)
	if err != nil || n != 0 || len(items) != 0 {
		t.Fatalf("invalid intake persisted: %d %v", n, err)
	}
}
