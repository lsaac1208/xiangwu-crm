package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestWorkbenchPrioritizesAllCandidatesBeforeLimitingTasks(t *testing.T) {
	for _, appointments := range []bool{false, true} {
		t.Run(map[bool]string{false: "overdue follow-ups", true: "overdue appointments and complete counts"}[appointments], func(t *testing.T) {
			db := newStage3TestDB(t)
			leads := repo.NewGormLeadRepository(db)
			appointmentRepo := repo.NewGormAppointmentRepository(db)
			ctx := context.Background()
			now := time.Date(2026, 10, 21, 12, 0, 0, 0, types.BusinessLocation)
			for i := int64(1); i <= 201; i++ {
				lead := stage3OwnedLead(i, "inviter-1")
				deadline := now.Add(-time.Hour)
				lead.UpdatedAt = now
				if i == 1 {
					deadline = now.Add(-48 * time.Hour)
					lead.UpdatedAt = now.Add(-30 * 24 * time.Hour)
				}
				if !appointments {
					lead.NextFollowupDueAt = &deadline
				}
				if err := leads.Create(ctx, lead); err != nil {
					t.Fatal(err)
				}
				if appointments {
					appointment := &types.LeadAppointment{AppointmentNo: fmt.Sprintf("priority-%d", i), LeadID: i, InviterUserID: "inviter-1", CloserUserID: "closer-1", Status: types.AppointmentScheduled, StartAt: deadline, EndAt: deadline.Add(time.Hour), Version: 1}
					if err := appointmentRepo.Create(ctx, appointment); err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&repo.LeadModel{}).Where("id = ?", i).UpdateColumn("current_appointment_id", appointment.ID).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			uc := NewGetWorkbenchUseCase(leads, appointmentRepo)
			uc.now = func() time.Time { return now }
			result, err := uc.Execute(ctx, "admin", "admin")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Tasks) != 50 {
				t.Fatalf("task count = %d", len(result.Tasks))
			}
			if result.Tasks[0].LeadID != 1 {
				t.Fatalf("oldest urgent task lost before limit: first=%+v", result.Tasks[0])
			}
			if appointments {
				if result.Summary.AppointmentUnconfirmed != 201 || result.Summary.TodayAppointments != 200 {
					t.Fatalf("appointment counts truncated: %+v", result.Summary)
				}
				invisible, err := uc.Execute(ctx, "another-inviter", "inviter")
				if err != nil || len(invisible.Tasks) != 0 || invisible.Summary.AppointmentUnconfirmed != 0 {
					t.Fatalf("out-of-scope appointment: %+v %v", invisible, err)
				}
				if err := db.Delete(&repo.LeadModel{}, 1).Error; err != nil {
					t.Fatal(err)
				}
				afterDelete, err := uc.Execute(ctx, "admin", "admin")
				if err != nil || afterDelete.Summary.AppointmentUnconfirmed != 200 || afterDelete.Tasks[0].LeadID == 1 {
					t.Fatalf("deleted lead appointment remained visible: %+v %v", afterDelete, err)
				}
			} else if result.Summary.OverdueFollowup != 201 {
				t.Fatalf("overdue count truncated: %+v", result.Summary)
			}
		})
	}
}
