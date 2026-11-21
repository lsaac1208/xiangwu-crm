package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

type mockFollowupRepository struct {
	followups []*types.LeadFollowup
}

func (m *mockFollowupRepository) Create(ctx context.Context, followup *types.LeadFollowup) error {
	m.followups = append(m.followups, followup)
	return nil
}

func (m *mockFollowupRepository) ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadFollowup, error) {
	var result []*types.LeadFollowup
	for _, followup := range m.followups {
		if followup.LeadID == leadID {
			result = append(result, followup)
		}
	}
	return result, nil
}

func (m *mockFollowupRepository) ListAll(ctx context.Context, offset, limit int) ([]*types.LeadFollowup, int, error) {
	return m.followups, len(m.followups), nil
}

func TestCreateFollowup_InvalidFirstFollowupUpdatesLeadOnce(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	followupRepo := &mockFollowupRepository{}
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID:          1,
		Status:      types.LeadStatusClaimed,
		Version:     1,
		OwnerUserID: "sales1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	uc := NewCreateFollowupUseCase(leadRepo, followupRepo)
	err := uc.Execute(context.Background(), &CreateFollowupRequest{
		LeadID:         1,
		SalesUserID:    "sales1",
		FollowupResult: "invalid",
		Remark:         "号码无效",
		IsValid:        false,
		InvalidReason:  "号码无效",
	})
	if err != nil {
		t.Fatalf("创建无效跟进失败: %v", err)
	}

	updatedLead, _ := leadRepo.GetByID(context.Background(), 1)
	if updatedLead.Status != types.LeadStatusInvalid {
		t.Fatalf("线索状态 = %s, want %s", updatedLead.Status, types.LeadStatusInvalid)
	}
	if updatedLead.BusinessStage != types.BusinessStageFinished || updatedLead.ResultStatus != types.LeadResultInvalid {
		t.Fatalf("workflow state = %s/%s, want finished/invalid", updatedLead.BusinessStage, updatedLead.ResultStatus)
	}
	if updatedLead.LastFollowupAt == nil {
		t.Fatal("last followup time was not written")
	}
	if updatedLead.FirstFollowupAt == nil {
		t.Fatalf("首次跟进时间未写入")
	}
	if updatedLead.Version != 2 {
		t.Fatalf("线索版本 = %d, want 2", updatedLead.Version)
	}
	if leadRepo.updateCalls != 1 {
		t.Fatalf("线索更新次数 = %d, want 1", leadRepo.updateCalls)
	}
	if len(followupRepo.followups) != 1 {
		t.Fatalf("跟进记录数 = %d, want 1", len(followupRepo.followups))
	}
}

func TestCreateFollowupRejectsTerminalLead(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	followupRepo := &mockFollowupRepository{}
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID:          1,
		Status:      types.LeadStatusConverted,
		Version:     2,
		OwnerUserID: "sales1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	uc := NewCreateFollowupUseCase(leadRepo, followupRepo)
	err := uc.Execute(context.Background(), &CreateFollowupRequest{
		LeadID:         1,
		SalesUserID:    "sales1",
		FollowupResult: "again",
		Remark:         "已成交后再次跟进",
		IsValid:        true,
	})
	if err != types.ErrLeadStatusInvalid {
		t.Fatalf("err = %v, want %v", err, types.ErrLeadStatusInvalid)
	}
	if len(followupRepo.followups) != 0 {
		t.Fatalf("跟进记录数 = %d, want 0", len(followupRepo.followups))
	}
	if leadRepo.updateCalls != 0 {
		t.Fatalf("线索更新次数 = %d, want 0", leadRepo.updateCalls)
	}
}

func TestCreateFollowupRejectsInvalidWithoutReason(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	followupRepo := &mockFollowupRepository{}
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID:          1,
		Status:      types.LeadStatusClaimed,
		Version:     1,
		OwnerUserID: "sales1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	uc := NewCreateFollowupUseCase(leadRepo, followupRepo)
	err := uc.Execute(context.Background(), &CreateFollowupRequest{
		LeadID:         1,
		SalesUserID:    "sales1",
		FollowupResult: "invalid",
		Remark:         "标记无效但未填原因",
		IsValid:        false,
		InvalidReason:  "   ", // 纯空白原因也应被拒绝
	})
	if err != types.ErrInvalidReasonRequired {
		t.Fatalf("err = %v, want %v", err, types.ErrInvalidReasonRequired)
	}
	if len(followupRepo.followups) != 0 {
		t.Fatalf("跟进记录数 = %d, want 0", len(followupRepo.followups))
	}
	if leadRepo.updateCalls != 0 {
		t.Fatalf("线索更新次数 = %d, want 0", leadRepo.updateCalls)
	}
}
