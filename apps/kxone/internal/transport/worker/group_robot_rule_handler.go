package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	notifpkg "github.com/lsaac128/kxone/internal/notification"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

const (
	groupRobotScanLimit   = 200
	groupRobotStaleAfter  = 5 * time.Minute
	groupRobotMaxAttempts = 4

	groupRobotPreRolloutReason = "business event predates group robot rules rollout"
)

var groupRobotRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type groupRobotRuleSettings interface {
	runtime.RuntimeSettings
	runtime.WorkflowFeatureFlags
	runtime.FollowupPolicySettings
	runtime.GroupRobotNotificationSettings
}

type preRolloutReminderSuppressor interface {
	SuppressCreatedBefore(ctx context.Context, reminderType string, createdBefore time.Time, reason string) (int64, error)
}

type leadReminderLookup interface {
	GetByID(ctx context.Context, id int64) (*types.LeadReminderRecord, error)
}

type GroupRobotRuleHandler struct {
	leads         repo.LeadRepository
	reminders     repo.LeadReminderRepository
	notifications repo.GroupRobotNotificationRepository
	outboxSources repo.GroupRobotOutboxReconciler
	bindings      repo.UserBindingRepository
	settings      groupRobotRuleSettings
	sender        any
	pool          LeadPoolAutoReturner
	publicBaseURL string
	rolloutMu     sync.Mutex
	rolloutCutoff time.Time
	rolloutInitAt time.Time
	now           func() time.Time
}

type groupRobotEventPayload struct {
	ReminderMinutes       int      `json:"reminder_minutes,omitempty"`
	FollowupOffsetMinutes int      `json:"followup_offset_minutes"`
	Reason                string   `json:"reason,omitempty"`
	PreviousOwnerID       string   `json:"previous_owner_user_id,omitempty"`
	ReceiverUserIDs       []string `json:"receiver_user_ids,omitempty"`
}

func NewGroupRobotRuleHandler(
	leads repo.LeadRepository,
	reminders repo.LeadReminderRepository,
	notifications repo.GroupRobotNotificationRepository,
	bindings repo.UserBindingRepository,
	settings groupRobotRuleSettings,
	sender any,
	pool LeadPoolAutoReturner,
	publicBaseURL string,
) *GroupRobotRuleHandler {
	initializedAt := time.Now()
	handler := &GroupRobotRuleHandler{
		leads: leads, reminders: reminders, notifications: notifications, bindings: bindings,
		settings: settings, sender: sender, pool: pool, publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		rolloutInitAt: initializedAt, now: time.Now,
	}
	if sources, ok := notifications.(repo.GroupRobotOutboxReconciler); ok {
		handler.outboxSources = sources
	}
	return handler
}

func (h *GroupRobotRuleHandler) WithOutboxReconciler(reconciler repo.GroupRobotOutboxReconciler) *GroupRobotRuleHandler {
	if h != nil {
		h.outboxSources = reconciler
	}
	return h
}

func (h *GroupRobotRuleHandler) HandleScan(ctx context.Context, _ *asynq.Task) error {
	if h == nil || h.notifications == nil || h.leads == nil || h.settings == nil || h.sender == nil {
		return nil
	}
	now := h.now()
	cutoff, err := h.ensureRolloutCutoff(ctx)
	if err != nil {
		return err
	}
	if recovered, err := h.notifications.RecoverStaleSending(ctx, now.Add(-groupRobotStaleAfter), now); err != nil {
		return fmt.Errorf("recover stale group robot notifications: %w", err)
	} else if recovered > 0 {
		slog.Warn("recovered stale group robot deliveries", "count", recovered)
	}
	if err := h.suppressPreRolloutBacklog(ctx); err != nil {
		return err
	}
	if err := h.reconcileMissingNewLeadEvents(ctx, cutoff, now); err != nil {
		return err
	}
	if err := h.reconcileMissingAutoPoolEvents(ctx, cutoff, now); err != nil {
		return err
	}
	if err := h.reconcileDispatchAndClaim(ctx, now); err != nil {
		return err
	}
	if err := h.bridgeFollowupReminders(ctx, now); err != nil {
		return err
	}
	if err := h.deliverReady(ctx, now); err != nil {
		return err
	}
	if h.pool != nil {
		if err := h.pool.AutoReturnOverdue(ctx, now, groupRobotScanLimit); err != nil {
			slog.Error("automatic public-pool scan failed", "err", err)
		}
		if err := h.reconcileMissingAutoPoolEvents(ctx, cutoff, h.now()); err != nil {
			return err
		}
		if err := h.deliverReady(ctx, h.now()); err != nil {
			return err
		}
	}
	return nil
}

func (h *GroupRobotRuleHandler) reconcileMissingNewLeadEvents(ctx context.Context, cutoff, now time.Time) error {
	if h.outboxSources == nil || cutoff.IsZero() {
		return nil
	}
	events, err := h.outboxSources.ListMissingNewLeadEvents(ctx, cutoff, groupRobotScanLimit)
	if err != nil {
		return fmt.Errorf("list new leads missing group robot notifications: %w", err)
	}
	for _, event := range events {
		if event == nil || event.LeadID <= 0 || event.CreatedAt.Before(cutoff) {
			continue
		}
		if err := h.createNotification(ctx, &types.GroupRobotNotification{
			EventKey: fmt.Sprintf("new-lead:%d", event.LeadID), Category: types.GroupRobotCategoryNewLead,
			EventType: types.GroupRobotEventNewLead, LeadID: event.LeadID, BusinessVersion: event.BusinessVersion,
			DueAt: event.CreatedAt, Status: types.GroupRobotNotificationPending,
			CreatedAt: event.CreatedAt, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("reconcile new-lead group robot notification: %w", err)
		}
	}
	return nil
}

func (h *GroupRobotRuleHandler) reconcileMissingAutoPoolEvents(ctx context.Context, cutoff, now time.Time) error {
	if h.outboxSources == nil || cutoff.IsZero() {
		return nil
	}
	events, err := h.outboxSources.ListMissingAutoPoolEvents(ctx, cutoff, groupRobotScanLimit)
	if err != nil {
		return fmt.Errorf("list automatic public-pool events missing group robot notifications: %w", err)
	}
	for _, event := range events {
		if event == nil || event.LeadID <= 0 || event.CreatedAt.Before(cutoff) ||
			strings.TrimSpace(event.ActorUserID) != "system" ||
			(event.Reason != types.PoolReasonUnclaimedTimeout && event.Reason != types.PoolReasonFollowupOverdue) {
			continue
		}
		payload, _ := json.Marshal(groupRobotEventPayload{
			Reason: event.Reason, PreviousOwnerID: event.PreviousOwnerUserID,
		})
		if err := h.createNotification(ctx, &types.GroupRobotNotification{
			EventKey: fmt.Sprintf("pool-auto-return:%d:v%d", event.LeadID, event.BusinessVersion),
			Category: types.GroupRobotCategoryPool, EventType: types.GroupRobotEventPoolAutoReturn,
			LeadID: event.LeadID, BusinessVersion: event.BusinessVersion, DueAt: event.CreatedAt,
			Status: types.GroupRobotNotificationPending, PayloadJSON: string(payload),
			CreatedAt: event.CreatedAt, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("reconcile automatic public-pool group robot notification: %w", err)
		}
	}
	return nil
}

func (h *GroupRobotRuleHandler) ensureRolloutCutoff(ctx context.Context) (time.Time, error) {
	h.rolloutMu.Lock()
	defer h.rolloutMu.Unlock()
	if !h.rolloutCutoff.IsZero() {
		return h.rolloutCutoff, nil
	}
	initializeAt := h.rolloutInitAt
	if initializeAt.IsZero() {
		initializeAt = h.now()
	}
	cutoff, err := h.settings.EnsureGroupRobotRulesRolloutCutoff(ctx, initializeAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("ensure group robot rules rollout cutoff: %w", err)
	}
	h.rolloutCutoff = cutoff
	return cutoff, nil
}

func (h *GroupRobotRuleHandler) suppressPreRolloutBacklog(ctx context.Context) error {
	h.rolloutMu.Lock()
	cutoff := h.rolloutCutoff
	h.rolloutMu.Unlock()
	if cutoff.IsZero() {
		return nil
	}
	categories := []string{
		types.GroupRobotCategoryDispatchClaim,
		types.GroupRobotCategoryFollowup,
		types.GroupRobotCategoryPool,
	}
	if _, err := h.notifications.SuppressCreatedBeforeCategories(ctx, categories, cutoff, groupRobotPreRolloutReason); err != nil {
		return fmt.Errorf("suppress pre-rollout rule notifications: %w", err)
	}
	if h.reminders != nil {
		var err error
		if suppressor, ok := h.reminders.(preRolloutReminderSuppressor); ok {
			_, err = suppressor.SuppressCreatedBefore(ctx, types.ReminderTypeFollowupDue, cutoff, groupRobotPreRolloutReason)
		} else {
			_, err = h.reminders.SuppressDue(ctx, types.ReminderTypeFollowupDue, cutoff, groupRobotPreRolloutReason)
		}
		if err != nil {
			return fmt.Errorf("suppress pre-rollout follow-up reminders: %w", err)
		}
	}
	return nil
}

func (h *GroupRobotRuleHandler) reconcileDispatchAndClaim(ctx context.Context, now time.Time) error {
	if _, err := h.ensureRolloutCutoff(ctx); err != nil {
		return err
	}
	if err := h.forEachLeadInStatus(ctx, string(types.LeadStatusPendingDispatch), func(lead *types.Lead) error {
		for _, delay := range h.settings.DispatchReminderDelays(ctx) {
			minutes := durationMinutes(delay)
			payload, _ := json.Marshal(groupRobotEventPayload{ReminderMinutes: minutes})
			dueAt := lead.CreatedAt.Add(delay)
			status, lastError := h.reconciledNotificationState(lead.CreatedAt)
			if err := h.createNotification(ctx, &types.GroupRobotNotification{
				EventKey: fmt.Sprintf("dispatch:%d:v%d:m%d", lead.ID, lead.Version, minutes),
				Category: types.GroupRobotCategoryDispatchClaim, EventType: types.GroupRobotEventDispatchOverdue,
				LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: dueAt,
				Status: status, LastError: lastError, PayloadJSON: string(payload), CreatedAt: lead.CreatedAt, UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reconcile pending-dispatch notifications: %w", err)
	}
	if err := h.forEachLeadInStatus(ctx, string(types.LeadStatusAssignedPendingClaim), func(lead *types.Lead) error {
		if lead.AssignedAt == nil {
			return nil
		}
		for _, delay := range h.settings.ClaimReminderDelays(ctx) {
			minutes := durationMinutes(delay)
			payload, _ := json.Marshal(groupRobotEventPayload{ReminderMinutes: minutes})
			dueAt := lead.AssignedAt.Add(delay)
			status, lastError := h.reconciledNotificationState(*lead.AssignedAt)
			if err := h.createNotification(ctx, &types.GroupRobotNotification{
				EventKey: fmt.Sprintf("claim:%d:v%d:m%d", lead.ID, lead.Version, minutes),
				Category: types.GroupRobotCategoryDispatchClaim, EventType: types.GroupRobotEventClaimOverdue,
				LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: dueAt,
				Status: status, LastError: lastError, PayloadJSON: string(payload), CreatedAt: *lead.AssignedAt, UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reconcile pending-claim notifications: %w", err)
	}
	return nil
}

// The legacy ListPending* methods only expose a fixed first page. Pending
// leads remain in the same state after ledger reconciliation, so repeatedly
// reading that page would starve all later records. The paginated admin query
// keeps a scan bounded while eventually reconciling every current lead.
func (h *GroupRobotRuleHandler) forEachLeadInStatus(ctx context.Context, status string, visit func(*types.Lead) error) error {
	for offset := 0; ; offset += groupRobotScanLimit {
		leads, total, err := h.leads.ListAllFiltered(ctx, repo.LeadListFilter{Status: status}, offset, groupRobotScanLimit)
		if err != nil {
			return err
		}
		for _, lead := range leads {
			if err := visit(lead); err != nil {
				return err
			}
		}
		if len(leads) < groupRobotScanLimit || offset+len(leads) >= total {
			return nil
		}
	}
}

func (h *GroupRobotRuleHandler) bridgeFollowupReminders(ctx context.Context, now time.Time) error {
	if h.reminders == nil {
		return nil
	}
	if _, err := h.ensureRolloutCutoff(ctx); err != nil {
		return err
	}
	records, err := h.reminders.ListDueByType(ctx, types.ReminderTypeFollowupDue, now, groupRobotScanLimit)
	if err != nil {
		return fmt.Errorf("list due follow-up reminders: %w", err)
	}
	for _, record := range records {
		if record == nil || record.ReminderType != types.ReminderTypeFollowupDue {
			continue
		}
		lead, err := h.leads.GetByID(ctx, record.LeadID)
		if err != nil {
			return err
		}
		if !isCurrentFollowupReminder(record, lead) {
			if err := h.reminders.MarkResult(ctx, record.ID, types.ReminderStatusCancelled, "business state changed", nil); err != nil {
				return err
			}
			continue
		}
		receivers := splitReceiverUserIDs(record.ReceiverUserID)
		offsetMinutes := int(record.DueAt.Sub(*lead.NextFollowupDueAt) / time.Minute)
		payload, _ := json.Marshal(groupRobotEventPayload{
			FollowupOffsetMinutes: offsetMinutes,
			ReceiverUserIDs:       receivers,
		})
		eventKey := fmt.Sprintf("followup-reminder:%d", record.ID)
		status, lastError := h.reconciledNotificationState(record.CreatedAt)
		notification := &types.GroupRobotNotification{
			EventKey: eventKey, Category: types.GroupRobotCategoryFollowup, EventType: types.GroupRobotEventFollowupDue,
			LeadID: record.LeadID, ReminderID: &record.ID, BusinessVersion: record.BusinessVersion,
			DueAt: record.DueAt, Status: status, LastError: lastError, PayloadJSON: string(payload),
			CreatedAt: record.CreatedAt, UpdatedAt: now,
		}
		if err := h.createNotification(ctx, notification); err != nil {
			return err
		}
		existing, err := h.notifications.GetByEventKey(ctx, eventKey)
		if err != nil {
			return err
		}
		if existing != nil && h.reconcileReminderTerminalState(ctx, record.ID, existing) {
			continue
		}
		if _, err := h.reminders.TryMarkSending(ctx, record.ID); err != nil {
			return err
		}
	}
	return nil
}

func (h *GroupRobotRuleHandler) reconciledNotificationState(eventCreatedAt time.Time) (string, string) {
	if !h.rolloutCutoff.IsZero() && eventCreatedAt.Before(h.rolloutCutoff) {
		return types.GroupRobotNotificationSuppressed, groupRobotPreRolloutReason
	}
	return types.GroupRobotNotificationPending, ""
}

func (h *GroupRobotRuleHandler) isPreRolloutRuleNotification(ctx context.Context, notification *types.GroupRobotNotification, cutoff time.Time) (bool, error) {
	if notification == nil || cutoff.IsZero() || notification.EventType == types.GroupRobotEventNewLead {
		return false, nil
	}
	eventCreatedAt := notification.CreatedAt
	switch notification.EventType {
	case types.GroupRobotEventDispatchOverdue:
		lead, err := h.leads.GetByID(ctx, notification.LeadID)
		if err != nil {
			return false, err
		}
		if lead != nil {
			eventCreatedAt = lead.CreatedAt
		}
	case types.GroupRobotEventClaimOverdue:
		lead, err := h.leads.GetByID(ctx, notification.LeadID)
		if err != nil {
			return false, err
		}
		if lead != nil && lead.AssignedAt != nil {
			eventCreatedAt = *lead.AssignedAt
		}
	case types.GroupRobotEventFollowupDue:
		if notification.ReminderID != nil && h.reminders != nil {
			if lookup, ok := h.reminders.(leadReminderLookup); ok {
				record, err := lookup.GetByID(ctx, *notification.ReminderID)
				if err != nil {
					return false, err
				}
				if record != nil {
					eventCreatedAt = record.CreatedAt
				}
			}
		}
	case types.GroupRobotEventPoolAutoReturn:
		// Pool notifications are written at the automatic return event time.
	default:
		return false, nil
	}
	return eventCreatedAt.Before(cutoff), nil
}

func (h *GroupRobotRuleHandler) reconcileReminderTerminalState(ctx context.Context, reminderID int64, notification *types.GroupRobotNotification) bool {
	if notification == nil {
		return false
	}
	switch notification.Status {
	case types.GroupRobotNotificationSent:
		_ = h.reminders.MarkResult(ctx, reminderID, types.ReminderStatusSent, "", notification.SentAt)
		return true
	case types.GroupRobotNotificationSuppressed:
		_ = h.reminders.MarkResult(ctx, reminderID, types.ReminderStatusSuppressed, notification.LastError, nil)
		return true
	case types.GroupRobotNotificationCancelled:
		_ = h.reminders.MarkResult(ctx, reminderID, types.ReminderStatusCancelled, notification.LastError, nil)
		return true
	case types.GroupRobotNotificationFailed:
		if notification.NextRetryAt == nil {
			_ = h.reminders.MarkResult(ctx, reminderID, types.ReminderStatusFailed, notification.LastError, nil)
			return true
		}
	}
	return false
}

func (h *GroupRobotRuleHandler) deliverReady(ctx context.Context, now time.Time) error {
	cutoff, err := h.ensureRolloutCutoff(ctx)
	if err != nil {
		return err
	}
	if err := h.suppressPreRolloutBacklog(ctx); err != nil {
		return err
	}
	notifications, err := h.notifications.ListReady(ctx, now, groupRobotScanLimit)
	if err != nil {
		return fmt.Errorf("list ready group robot notifications: %w", err)
	}
	for _, notification := range notifications {
		claimed, err := h.notifications.TryMarkSending(ctx, notification.ID)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		notification.Attempts++
		preRollout, err := h.isPreRolloutRuleNotification(ctx, notification, cutoff)
		if err != nil {
			h.failDelivery(ctx, notification, err)
			continue
		}
		if preRollout {
			if err := h.notifications.MarkSuppressed(ctx, notification.ID, groupRobotPreRolloutReason); err != nil {
				return err
			}
			h.markLinkedReminder(ctx, notification, types.ReminderStatusSuppressed, groupRobotPreRolloutReason, nil)
			continue
		}
		valid, reason, lead, err := h.validateCurrentNotification(ctx, notification)
		if err != nil {
			h.failDelivery(ctx, notification, err)
			continue
		}
		if !valid {
			if err := h.notifications.MarkCancelled(ctx, notification.ID, reason); err != nil {
				return err
			}
			h.markLinkedReminder(ctx, notification, types.ReminderStatusCancelled, reason, nil)
			continue
		}
		if !h.settings.GroupRobotCategoryEnabled(ctx, notification.Category) {
			reason = "notification category is disabled"
			if err := h.notifications.MarkSuppressed(ctx, notification.ID, reason); err != nil {
				return err
			}
			h.markLinkedReminder(ctx, notification, types.ReminderStatusSuppressed, reason, nil)
			continue
		}
		content, err := h.renderNotification(ctx, notification, lead, now)
		if err != nil {
			h.failDelivery(ctx, notification, err)
			continue
		}
		message := wecom.GroupRobotMessage{Content: content}
		if h.settings != nil && h.settings.GroupRobotMentionsEnabled(ctx) {
			message.MentionUserIDs = h.resolveMentionUserIDs(ctx, notification, lead)
		}
		if err := sendGroupRobotMessage(ctx, h.sender, message); err != nil {
			h.failDelivery(ctx, notification, err)
			continue
		}
		sentAt := h.now()
		if err := h.notifications.MarkSent(ctx, notification.ID, sentAt); err != nil {
			return err
		}
		h.markLinkedReminder(ctx, notification, types.ReminderStatusSent, "", &sentAt)
	}
	return nil
}

func (h *GroupRobotRuleHandler) failDelivery(ctx context.Context, notification *types.GroupRobotNotification, deliveryErr error) {
	var nextRetryAt *time.Time
	if notification.Attempts < groupRobotMaxAttempts {
		next := h.now().Add(groupRobotRetryDelays[notification.Attempts-1])
		nextRetryAt = &next
	}
	if err := h.notifications.MarkFailed(ctx, notification.ID, deliveryErr.Error(), nextRetryAt); err != nil {
		slog.Error("mark group robot delivery failed", "notification_id", notification.ID, "err", err)
	}
	if nextRetryAt == nil {
		h.markLinkedReminder(ctx, notification, types.ReminderStatusFailed, deliveryErr.Error(), nil)
	}
	slog.Error("group robot notification delivery failed", "notification_id", notification.ID, "event_type", notification.EventType, "attempt", notification.Attempts, "err", deliveryErr)
}

func (h *GroupRobotRuleHandler) validateCurrentNotification(ctx context.Context, notification *types.GroupRobotNotification) (bool, string, *types.Lead, error) {
	lead, err := h.leads.GetByID(ctx, notification.LeadID)
	if err != nil {
		return false, "", nil, err
	}
	if lead == nil {
		return false, "lead no longer exists", nil, nil
	}
	payload := decodeGroupRobotPayload(notification.PayloadJSON)
	switch notification.EventType {
	case types.GroupRobotEventNewLead:
		// New-lead is a creation fact, not a state reminder. A fast assignment
		// after creation must not erase the promised "entered CRM" notice.
		return true, "", lead, nil
	case types.GroupRobotEventDispatchAssigned:
		if lead.Status != types.LeadStatusAssignedPendingClaim || lead.Version != notification.BusinessVersion ||
			strings.TrimSpace(firstNonEmptyWorker(lead.AssignedInviterUserID, lead.AssignedSalesUserID)) == "" {
			return false, "lead is no longer assigned to the matching user", lead, nil
		}
	case types.GroupRobotEventDispatchOverdue:
		if lead.Status != types.LeadStatusPendingDispatch || lead.Version != notification.BusinessVersion {
			return false, "lead is no longer pending dispatch", lead, nil
		}
		if !configuredReminderMinute(payload.ReminderMinutes, h.settings.DispatchReminderDelays(ctx)) {
			return false, "dispatch reminder rule changed or disabled", lead, nil
		}
	case types.GroupRobotEventClaimOverdue:
		if lead.Status != types.LeadStatusAssignedPendingClaim || lead.Version != notification.BusinessVersion || lead.AssignedAt == nil {
			return false, "lead is no longer pending claim", lead, nil
		}
		if !configuredReminderMinute(payload.ReminderMinutes, h.settings.ClaimReminderDelays(ctx)) {
			return false, "claim reminder rule changed or disabled", lead, nil
		}
	case types.GroupRobotEventFollowupDue:
		if lead.Version != notification.BusinessVersion || !types.IsCurrentFollowupEligible(lead) {
			return false, "follow-up business state changed", lead, nil
		}
		if !configuredReminderMinute(payload.FollowupOffsetMinutes, h.settings.FollowupReminderOffsets(ctx)) ||
			!sameReminderDueAt(notification.DueAt, lead.NextFollowupDueAt.Add(time.Duration(payload.FollowupOffsetMinutes)*time.Minute)) {
			return false, "follow-up reminder rule changed or disabled", lead, nil
		}
	case types.GroupRobotEventPoolAutoReturn:
		if lead.Version != notification.BusinessVersion || lead.OwnershipState != types.OwnershipPublicPool {
			return false, "lead is no longer in the matching public-pool version", lead, nil
		}
	default:
		return false, "unknown notification event type", lead, nil
	}
	return true, "", lead, nil
}

func (h *GroupRobotRuleHandler) renderNotification(ctx context.Context, notification *types.GroupRobotNotification, lead *types.Lead, now time.Time) (string, error) {
	payload := decodeGroupRobotPayload(notification.PayloadJSON)
	link := h.leadURL(lead.ID)
	common := []string{
		"线索编号：" + safeGroupRobotText(lead.LeadNo),
		"客户：" + safeGroupRobotText(lead.CustomerName),
		"手机：" + safeGroupRobotText(maskedNotificationMobile(lead)),
		"微信：" + safeGroupRobotText(maskedNotificationWechat(lead)),
		"来源：" + safeGroupRobotText(lead.SourceChannel),
		"状态：" + safeGroupRobotText(groupRobotLeadStatusText(lead)),
	}
	var title string
	var detail []string
	switch notification.EventType {
	case types.GroupRobotEventNewLead:
		title = "【新线索进入 CRM】"
		ownerUserID := firstNonEmptyWorker(lead.CurrentOwnerUserID, lead.AssignedInviterUserID, lead.AssignedSalesUserID, lead.DispatchManagerUserID, lead.CreatedByUserID)
		if lead.Status == types.LeadStatusPendingDispatch {
			ownerUserID = lead.DispatchManagerUserID
		}
		owner := "责任主管"
		if strings.TrimSpace(ownerUserID) != "" {
			var err error
			owner, err = h.displayName(ctx, ownerUserID)
			if err != nil {
				return "", err
			}
		}
		detail = []string{
			"负责人：@" + safeGroupRobotText(owner),
			"规则：新线索即时通知",
			"到期时间：" + formatGroupRobotTime(notification.DueAt),
			"项目：" + safeGroupRobotText(lead.ProductName),
		}
	case types.GroupRobotEventDispatchAssigned:
		title = "【线索已派发】"
		owner, err := h.displayName(ctx, firstNonEmptyWorker(lead.AssignedInviterUserID, lead.AssignedSalesUserID))
		if err != nil {
			return "", err
		}
		detail = []string{"负责人：@" + safeGroupRobotText(owner), "规则：派发成功即时通知", "派发时间：" + formatGroupRobotTime(notification.DueAt), "项目：" + safeGroupRobotText(lead.ProductName)}
	case types.GroupRobotEventDispatchOverdue:
		title = "【待分配线索提醒】"
		manager, err := h.displayName(ctx, lead.DispatchManagerUserID)
		if err != nil {
			return "", err
		}
		detail = []string{
			"负责人：@" + safeGroupRobotText(manager),
			"规则：创建后 " + strconv.Itoa(payload.ReminderMinutes) + " 分钟仍未分配",
			"到期时间：" + formatGroupRobotTime(notification.DueAt),
			"已等待：" + strconv.Itoa(nonNegativeMinutes(now.Sub(lead.CreatedAt))) + " 分钟",
		}
	case types.GroupRobotEventClaimOverdue:
		title = "【已分配待领取提醒】"
		owner, err := h.displayName(ctx, firstNonEmptyWorker(lead.AssignedInviterUserID, lead.AssignedSalesUserID))
		if err != nil {
			return "", err
		}
		detail = []string{
			"负责人：@" + safeGroupRobotText(owner),
			"规则：分配后 " + strconv.Itoa(payload.ReminderMinutes) + " 分钟仍未领取",
			"到期时间：" + formatGroupRobotTime(notification.DueAt),
		}
		if lead.AssignedAt != nil {
			detail = append(detail, "已等待："+strconv.Itoa(nonNegativeMinutes(now.Sub(*lead.AssignedAt)))+" 分钟")
		}
	case types.GroupRobotEventFollowupDue:
		title = "【回访逾期提醒】"
		names, err := h.displayNames(ctx, payload.ReceiverUserIDs)
		if err != nil {
			return "", err
		}
		detail = []string{
			"负责人：@" + safeGroupRobotText(strings.Join(names, "、")),
			"规则：" + strconv.Itoa(lead.StarRating) + " 星线索回访到期",
			"到期时间：" + formatGroupRobotTime(notification.DueAt),
			"已逾期：" + strconv.Itoa(nonNegativeMinutes(now.Sub(notification.DueAt))) + " 分钟",
		}
	case types.GroupRobotEventPoolAutoReturn:
		title = "【线索已自动进入公海】"
		previousOwner, err := h.displayName(ctx, payload.PreviousOwnerID)
		if err != nil {
			return "", err
		}
		reason := poolReasonText(payload.Reason)
		detail = []string{
			"负责人：@" + safeGroupRobotText(previousOwner),
			"规则：自动回公海（" + safeGroupRobotText(reason) + "）",
			"到期时间：" + formatGroupRobotTime(notification.DueAt),
			"原因：" + safeGroupRobotText(reason),
		}
	}
	lines := append([]string{title, ""}, common...)
	lines = append(lines, detail...)
	if link != "" {
		lines = append(lines, "", "详情："+link)
	}
	return strings.Join(lines, "\n"), nil
}

func (h *GroupRobotRuleHandler) createNotification(ctx context.Context, notification *types.GroupRobotNotification) error {
	_, err := h.notifications.Create(ctx, notification)
	return err
}

func (h *GroupRobotRuleHandler) markLinkedReminder(ctx context.Context, notification *types.GroupRobotNotification, status, message string, sentAt *time.Time) {
	if h.reminders == nil || notification.ReminderID == nil {
		return
	}
	if err := h.reminders.MarkResult(ctx, *notification.ReminderID, status, message, sentAt); err != nil {
		slog.Error("update linked reminder status failed", "reminder_id", *notification.ReminderID, "status", status, "err", err)
	}
}

func (h *GroupRobotRuleHandler) displayName(ctx context.Context, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "未指定", nil
	}
	if h.bindings == nil {
		return userID, nil
	}
	binding, err := h.bindings.GetByWeComUserID(ctx, userID)
	if err != nil {
		return "", err
	}
	if binding != nil && strings.TrimSpace(binding.EmployeeName) != "" {
		return binding.EmployeeName, nil
	}
	return userID, nil
}

func sendGroupRobotMessage(ctx context.Context, sender any, message wecom.GroupRobotMessage) error {
	switch typed := sender.(type) {
	case wecom.GroupRobotSender:
		return typed.Send(ctx, message)
	case wecom.GroupRobotMarkdownSender:
		return typed.SendMarkdown(ctx, message.Content)
	default:
		return fmt.Errorf("group robot sender is not configured")
	}
}

func (h *GroupRobotRuleHandler) resolveMentionUserIDs(ctx context.Context, n *types.GroupRobotNotification, lead *types.Lead) []string {
	if n == nil || lead == nil {
		return nil
	}
	actorIDs := h.mentionActorIDs(ctx, n, lead)
	resolved, err := notifpkg.ResolveMentionRecipients(ctx, h.bindings, actorIDs)
	if err != nil {
		slog.Warn("resolve group robot mention recipients failed", "lead_id", lead.ID, "err", err)
		return nil
	}
	if len(resolved.MissingNames) > 0 {
		slog.Warn("group robot mention recipient has no UserID", "lead_id", lead.ID, "names", resolved.MissingNames)
	}
	return resolved.MentionUserIDs
}

func (h *GroupRobotRuleHandler) mentionActorIDs(ctx context.Context, n *types.GroupRobotNotification, lead *types.Lead) []string {
	result := make([]string, 0, 4)
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, existing := range result {
			if existing == id {
				return
			}
		}
		result = append(result, id)
	}
	appendManagers := func() {
		managers, err := notifpkg.ActiveManagerUserIDs(ctx, h.bindings)
		if err != nil {
			slog.Warn("list active managers for group robot mention failed", "err", err)
			return
		}
		for _, id := range managers {
			appendID(id)
		}
	}
	appendManager := func() {
		if strings.TrimSpace(lead.DispatchManagerUserID) != "" {
			appendID(lead.DispatchManagerUserID)
			return
		}
		appendManagers()
	}
	payload := decodeGroupRobotPayload(n.PayloadJSON)
	switch n.EventType {
	case types.GroupRobotEventNewLead:
		if lead.DispatchManagerUserID != "" {
			appendID(lead.DispatchManagerUserID)
		} else {
			appendManagers()
		}
	case types.GroupRobotEventDispatchAssigned:
		appendID(firstNonEmptyWorker(lead.AssignedInviterUserID, lead.AssignedSalesUserID))
	case types.GroupRobotEventClaimOverdue:
		appendID(firstNonEmptyWorker(lead.AssignedInviterUserID, lead.AssignedSalesUserID))
		if isFinalReminder(payload.ReminderMinutes, h.settings.ClaimReminderDelays(ctx)) {
			appendManager()
		}
	case types.GroupRobotEventDispatchOverdue:
		appendManager()
	case types.GroupRobotEventFollowupDue:
		for _, id := range payload.ReceiverUserIDs {
			appendID(id)
		}
		if isFinalReminder(payload.FollowupOffsetMinutes, h.settings.FollowupReminderOffsets(ctx)) {
			appendManager()
		}
	case types.GroupRobotEventPoolAutoReturn:
		appendID(payload.PreviousOwnerID)
		appendManager()
	}
	return result
}

func isFinalReminder(value int, delays []time.Duration) bool {
	if len(delays) == 0 {
		return false
	}
	maxValue := 0
	for _, delay := range delays {
		if durationMinutes(delay) > maxValue {
			maxValue = durationMinutes(delay)
		}
	}
	return value == maxValue
}

func (h *GroupRobotRuleHandler) displayNames(ctx context.Context, ids []string) ([]string, error) {
	result := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		name, err := h.displayName(ctx, id)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	if len(result) == 0 {
		result = append(result, "未指定")
	}
	return result, nil
}

func (h *GroupRobotRuleHandler) leadURL(leadID int64) string {
	if h.publicBaseURL == "" || leadID <= 0 {
		return ""
	}
	parsed, err := url.Parse(h.publicBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/leads/" + strconv.FormatInt(leadID, 10)
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

func decodeGroupRobotPayload(raw string) groupRobotEventPayload {
	var payload groupRobotEventPayload
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload
}

func configuredReminderMinute(minutes int, delays []time.Duration) bool {
	for _, delay := range delays {
		if durationMinutes(delay) == minutes {
			return true
		}
	}
	return false
}

func sameReminderDueAt(left, right time.Time) bool {
	difference := left.Sub(right)
	if difference < 0 {
		difference = -difference
	}
	return difference <= time.Second
}

func splitReceiverUserIDs(value string) []string {
	parts := strings.Split(value, "|")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func maskedNotificationMobile(lead *types.Lead) string {
	if lead == nil {
		return ""
	}
	value := strings.TrimSpace(lead.CustomerMobile)
	if value == "" {
		value = strings.TrimSpace(lead.CustomerMobileMasked)
	}
	runes := []rune(value)
	if len(runes) < 7 {
		return maskShortSensitiveValue(value)
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

func maskedNotificationWechat(lead *types.Lead) string {
	if lead == nil {
		return ""
	}
	value := strings.TrimSpace(lead.CustomerWechat)
	if value == "" {
		value = strings.TrimSpace(lead.CustomerWechatMasked)
	}
	return maskShortSensitiveValue(value)
}

func maskShortSensitiveValue(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) == 1 {
		return "*"
	}
	if len(runes) <= 4 {
		return string(runes[:1]) + "**" + string(runes[len(runes)-1:])
	}
	visible := 2
	return string(runes[:visible]) + "****" + string(runes[len(runes)-visible:])
}

func formatGroupRobotTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	chinaStandardTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	return value.In(chinaStandardTime).Format("2006-01-02 15:04")
}

func groupRobotLeadStatusText(lead *types.Lead) string {
	if lead == nil {
		return "-"
	}
	labels := map[types.LeadStatus]string{
		types.LeadStatusPendingDispatch:      "待分配",
		types.LeadStatusAssignedPendingClaim: "已分配待领取",
		types.LeadStatusClaimed:              "已领取",
		types.LeadStatusFollowing:            "跟进中",
		types.LeadStatusPublicPool:           "公海",
		types.LeadStatusInvalid:              "无效",
		types.LeadStatusConverted:            "已转化",
		types.LeadStatusClosed:               "已关闭",
	}
	if label := labels[lead.Status]; label != "" {
		return label
	}
	return string(lead.Status)
}

func safeGroupRobotText(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", ">", "＞", "<", "＜").Replace(value))
	if value == "" {
		return "-"
	}
	runes := []rune(value)
	if len(runes) > 160 {
		value = string(runes[:160]) + "…"
	}
	return value
}

func poolReasonText(reason string) string {
	switch reason {
	case types.PoolReasonUnclaimedTimeout:
		return "分配后长时间未领取"
	case types.PoolReasonFollowupOverdue:
		return "回访逾期超过宽限时间"
	default:
		return safeGroupRobotText(reason)
	}
}

func nonNegativeMinutes(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int(duration.Minutes())
}

func firstNonEmptyWorker(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
