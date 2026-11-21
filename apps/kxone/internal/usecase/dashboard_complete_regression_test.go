package usecase

import (
	"fmt"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"testing"
	"time"
)

func TestDashboardReportsEveryLeadBeyondTenThousand(t *testing.T) {
	for _, size := range []int{10001, 20001} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db := newStage3TestDB(t)
			now := time.Now().UTC()
			models := make([]repo.LeadModel, size)
			for i := range models {
				models[i] = repo.LeadModel{LeadNo: fmt.Sprintf("audit-large-%d", i), CustomerName: "synthetic", Status: string(types.LeadStatusPendingDispatch), CreatedAt: now.Add(time.Duration(i) * time.Second), UpdatedAt: now}
			}
			models[0].Status = string(types.LeadStatusConverted)
			models[0].DealAmount = 12345
			if err := db.CreateInBatches(models, 100).Error; err != nil {
				t.Fatal(err)
			}
			var persisted int64
			if err := db.Model(&repo.LeadModel{}).Count(&persisted).Error; err != nil {
				t.Fatal(err)
			}
			uc := NewGetDashboardStatsUseCase(repo.NewGormLeadRepository(db), repo.NewGormFollowupRepository(db))
			got, err := uc.Execute(t.Context(), &GetDashboardStatsRequest{CurrentUserID: "admin", Role: "admin"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("persisted leads=%d; dashboard total=%d, converted=%d, deal amount=%.2f; omitted oldest actual converted amount=12345.00", persisted, got.Overview.TotalLeads, got.Overview.Converted, got.Overview.TotalDealAmount)
			if got.Overview.TotalLeads != int(persisted) || got.Overview.Converted != 1 || got.Overview.TotalDealAmount != 12345 {
				t.Errorf("BUG: default dashboard silently excludes records beyond latest 10000")
			}
		})
	}
}
