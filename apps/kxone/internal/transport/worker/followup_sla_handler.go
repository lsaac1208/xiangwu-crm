package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
)

func (h *TaskHandler) HandleScanFollowupSLA(ctx context.Context, _ *asynq.Task) error {
	if h.reminderRepo != nil {
		records, err := h.reminderRepo.ListDue(ctx, time.Now(), 200)
		if err != nil {
			return fmt.Errorf("scan follow-up reminders: %w", err)
		}
		for _, record := range records {
			if err := h.processFollowupReminder(ctx, record); err != nil {
				slog.Error("process follow-up reminder failed", "reminder_id", record.ID, "lead_id", record.LeadID, "err", err)
			}
		}
	}
	if h.poolAutoReturner != nil {
		if err := h.poolAutoReturner.AutoReturnOverdue(ctx, time.Now(), 200); err != nil {
			return fmt.Errorf("auto return overdue leads: %w", err)
		}
	}
	return nil
}

func (h *TaskHandler) processFollowupReminder(ctx context.Context, record *types.LeadReminderRecord) error {
	claimed, err := h.reminderRepo.TryMarkSending(ctx, record.ID)
	if err != nil || !claimed {
		return err
	}
	lead, err := h.leadRepo.GetByID(ctx, record.LeadID)
	if err != nil {
		return h.failReminder(ctx, record.ID, err)
	}
	if !h.isCurrentReminder(ctx, record, lead) {
		return h.reminderRepo.MarkResult(ctx, record.ID, types.ReminderStatusCancelled, "business state changed", nil)
	}
	if record.ReminderType == types.ReminderTypeAppointmentUpcoming || record.ReminderType == types.ReminderTypeAppointmentUnconfirmed {
		return h.sendAppointmentReminder(ctx, record, lead)
	}
	overdue := time.Since(record.DueAt)
	if overdue < 0 {
		overdue = 0
	}
	content := fmt.Sprintf("【回访提醒】\n\n线索编号：%s\n客户：%s\n当前星级：%d 星\n已逾期：%d 分钟\n\n请尽快添加跟进记录。", lead.LeadNo, lead.CustomerName, lead.StarRating, int(overdue.Minutes()))
	if err := h.wecomClient.SendText(ctx, &wecom.SendTextRequest{ToUser: record.ReceiverUserID, AgentID: h.runtimeSettings.WeComAgentID(ctx), Content: content}); err != nil {
		return h.failReminder(ctx, record.ID, err)
	}
	now := time.Now()
	return h.reminderRepo.MarkResult(ctx, record.ID, types.ReminderStatusSent, "", &now)
}

func (h *TaskHandler) sendAppointmentReminder(ctx context.Context, record *types.LeadReminderRecord, lead *types.Lead) error {
	title := "预约即将开始"
	description := "请提前准备接待并完成线下信息交接。"
	if record.ReminderType == types.ReminderTypeAppointmentUnconfirmed {
		title = "预约时间已到，尚未确认到访"
		description = "请邀约师及时确认到访或登记爽约。"
	}
	content := fmt.Sprintf("【%s】\n\n线索编号：%s\n业务阶段：%s\n\n%s", title, lead.LeadNo, lead.BusinessStage, description)
	if err := h.wecomClient.SendText(ctx, &wecom.SendTextRequest{ToUser: record.ReceiverUserID, AgentID: h.runtimeSettings.WeComAgentID(ctx), Content: content}); err != nil {
		return h.failReminder(ctx, record.ID, err)
	}
	now := time.Now()
	return h.reminderRepo.MarkResult(ctx, record.ID, types.ReminderStatusSent, "", &now)
}

func (h *TaskHandler) failReminder(ctx context.Context, id int64, err error) error {
	if markErr := h.reminderRepo.MarkResult(ctx, id, types.ReminderStatusFailed, err.Error(), nil); markErr != nil {
		return markErr
	}
	return err
}

func isCurrentFollowupReminder(record *types.LeadReminderRecord, lead *types.Lead) bool {
	return record != nil && record.ReminderType == types.ReminderTypeFollowupDue && types.IsCurrentFollowupEligible(lead) && record.BusinessVersion == lead.Version

}

func (h *TaskHandler) isCurrentReminder(ctx context.Context, record *types.LeadReminderRecord, lead *types.Lead) bool {
	if record == nil {
		return false
	}
	if record.ReminderType == types.ReminderTypeFollowupDue {
		return isCurrentFollowupReminder(record, lead)
	}
	if (record.ReminderType != types.ReminderTypeAppointmentUpcoming && record.ReminderType != types.ReminderTypeAppointmentUnconfirmed) ||
		record.AppointmentID == nil || h.appointmentRepo == nil {
		return false
	}
	appointment, err := h.appointmentRepo.GetByID(ctx, *record.AppointmentID)
	return err == nil && appointment != nil && appointment.Status == types.AppointmentScheduled &&
		appointment.Version == record.BusinessVersion && appointment.LeadID == record.LeadID &&
		lead != nil && lead.ResultStatus == types.LeadResultActive && lead.BusinessStage == types.BusinessStageInvitation && lead.CurrentAppointmentID != nil && *lead.CurrentAppointmentID == appointment.ID && lead.OwnershipState != types.OwnershipAssignedPendingClaim && lead.CurrentOwnerUserID == appointment.InviterUserID &&
		(record.ReceiverUserID == appointment.InviterUserID || record.ReceiverUserID == appointment.CloserUserID || (record.ReminderType == types.ReminderTypeAppointmentUnconfirmed && record.ReceiverUserID == lead.DispatchManagerUserID))
}
