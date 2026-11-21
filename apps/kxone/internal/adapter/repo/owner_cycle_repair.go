package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
	"sort"
	"time"
)

type OwnerCycleRepairValues struct {
	Status           string     `json:"status"`
	StartedAt        *time.Time `json:"current_owner_started_at"`
	OwnerFirstAt     *time.Time `json:"current_owner_first_followup_at"`
	LifecycleFirstAt *time.Time `json:"first_followup_at"`
}
type RepairEvidence struct {
	Table string    `json:"table"`
	ID    int64     `json:"id"`
	Event string    `json:"event"`
	At    time.Time `json:"at"`
}
type OwnerCycleRepairItem struct {
	LeadID                 int64                  `json:"lead_id"`
	ExpectedVersion        int                    `json:"expected_version"`
	ExpectedProfileVersion int                    `json:"expected_profile_version"`
	Before                 OwnerCycleRepairValues `json:"before"`
	After                  OwnerCycleRepairValues `json:"after"`
	Evidence               []RepairEvidence       `json:"evidence"`
}
type OwnerCycleRepairPlan struct {
	GeneratedAt     time.Time              `json:"generated_at"`
	Items           []OwnerCycleRepairItem `json:"items"`
	WithoutEvidence int                    `json:"without_evidence"`
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC()
	return &v
}
func repairValues(lead LeadModel) OwnerCycleRepairValues {
	return OwnerCycleRepairValues{lead.Status, utcPointer(lead.CurrentOwnerStartedAt), utcPointer(lead.CurrentOwnerFirstFollowupAt), utcPointer(lead.FirstFollowupAt)}
}
func sameRepairValues(a, b OwnerCycleRepairValues) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}

func PlanOwnerCycleRepairs(ctx context.Context, db *gorm.DB) (*OwnerCycleRepairPlan, error) {
	var leads []LeadModel
	if err := db.WithContext(ctx).Order("id").Find(&leads).Error; err != nil {
		return nil, err
	}
	plan := &OwnerCycleRepairPlan{GeneratedAt: time.Now().UTC(), Items: []OwnerCycleRepairItem{}}
	for _, lead := range leads {
		item, err := planOwnerCycleRepair(ctx, db, lead)
		if err != nil {
			return nil, err
		}
		if item != nil {
			plan.Items = append(plan.Items, *item)
		} else if lead.CurrentOwnerUserID != "" && lead.CurrentOwnerStartedAt == nil {
			plan.WithoutEvidence++
		}
	}
	return plan, nil
}
func planOwnerCycleRepair(ctx context.Context, db *gorm.DB, lead LeadModel) (*OwnerCycleRepairItem, error) {
	item := &OwnerCycleRepairItem{LeadID: lead.ID, ExpectedVersion: lead.Version, ExpectedProfileVersion: lead.ProfileVersion, Before: repairValues(lead), After: repairValues(lead)}
	var followups []LeadFollowupModel
	if err := db.WithContext(ctx).Where("lead_id = ?", lead.ID).Order("created_at, id").Find(&followups).Error; err != nil {
		return nil, err
	}
	for _, f := range followups {
		if item.After.LifecycleFirstAt == nil && !f.CreatedAt.Before(lead.CreatedAt) {
			item.After.LifecycleFirstAt = utcPointer(&f.CreatedAt)
			item.Evidence = append(item.Evidence, RepairEvidence{"lead_followups", f.ID, "lifecycle_first", f.CreatedAt.UTC()})
			break
		}
	}
	type ownershipEvent struct {
		owner    string
		claimed  bool
		evidence RepairEvidence
	}
	events := []ownershipEvent{}
	var dispatches []LeadDispatchRecordModel
	if err := db.WithContext(ctx).Where("lead_id = ?", lead.ID).Find(&dispatches).Error; err != nil {
		return nil, err
	}
	for _, d := range dispatches {
		claimed := d.ActionType == "claim" || d.ActionType == "self_create" || d.ActionType == "self_assign"
		// The manager's explicit takeover records 'reassign' and an immediate claim timestamp.
		if d.ActionType == "reassign" && d.ManagerUserID == d.SalesUserID && lead.ClaimedAt != nil && d.CreatedAt.Equal(*lead.ClaimedAt) {
			claimed = true
		}
		if d.ActionType == "claim" || d.ActionType == "self_create" || d.ActionType == "self_assign" || d.ActionType == "assign" || d.ActionType == "reassign" {
			events = append(events, ownershipEvent{d.SalesUserID, claimed, RepairEvidence{"lead_dispatch_records", d.ID, d.ActionType, d.CreatedAt.UTC()}})
		}
	}
	var pools []LeadPoolRecordModel
	if err := db.WithContext(ctx).Where("lead_id = ?", lead.ID).Find(&pools).Error; err != nil {
		return nil, err
	}
	for _, p := range pools {
		events = append(events, ownershipEvent{p.ActorUserID, p.Action == types.PoolActionClaim, RepairEvidence{"lead_pool_records", p.ID, p.Action, p.CreatedAt.UTC()}})
	}
	var activities []LeadActivityRecordModel
	if err := db.WithContext(ctx).Where("lead_id = ? AND activity_type IN ?", lead.ID, []string{types.LeadActivityArrivedDelivered, types.LeadActivityCloserReassigned}).Find(&activities).Error; err != nil {
		return nil, err
	}
	for _, a := range activities {
		var after map[string]any
		if json.Unmarshal([]byte(a.ToValueJSON), &after) != nil {
			continue
		}
		owner, _ := after["owner_userid"].(string)
		if owner != "" {
			events = append(events, ownershipEvent{owner, true, RepairEvidence{"lead_activity_records", a.ID, a.ActivityType, a.CreatedAt.UTC()}})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].evidence.At.Before(events[j].evidence.At) })
	if lead.CurrentOwnerUserID != "" && lead.OwnershipState == string(types.OwnershipPrivate) && len(events) > 0 {
		latest := events[len(events)-1]
		unambiguous := true
		for _, e := range events {
			if e.evidence.At.Equal(latest.evidence.At) && (e.owner != latest.owner || e.claimed != latest.claimed) {
				unambiguous = false
			}
		}
		if unambiguous && latest.claimed && latest.owner == lead.CurrentOwnerUserID && !latest.evidence.At.Before(lead.CreatedAt) && !latest.evidence.At.After(time.Now()) {
			if item.After.StartedAt == nil {
				item.After.StartedAt = utcPointer(&latest.evidence.At)
			}
			item.Evidence = append(item.Evidence, latest.evidence)
			for _, f := range followups {
				if f.SalesUserID != lead.CurrentOwnerUserID || f.CreatedAt.Before(*item.After.StartedAt) || (f.BusinessStage != "" && f.BusinessStage != lead.BusinessStage) {
					continue
				}
				if item.After.OwnerFirstAt == nil {
					item.After.OwnerFirstAt = utcPointer(&f.CreatedAt)
					item.Evidence = append(item.Evidence, RepairEvidence{"lead_followups", f.ID, "owner_first", f.CreatedAt.UTC()})
				}
				if lead.Status == string(types.LeadStatusClaimed) && f.IsValid {
					item.After.Status = string(types.LeadStatusFollowing)
					item.Evidence = append(item.Evidence, RepairEvidence{"lead_followups", f.ID, "valid_owner_followup", f.CreatedAt.UTC()})
					break
				}
			}
		}
	}
	if sameRepairValues(item.Before, item.After) {
		return nil, nil
	}
	return item, nil
}

// ApplyOwnerCycleRepair verifies evidence again and writes only the four planned fields.
// The caller may rebuild reminders inside this same transaction using afterWrite.
func ApplyOwnerCycleRepair(ctx context.Context, db *gorm.DB, item OwnerCycleRepairItem, afterWrite func(*gorm.DB, *types.Lead) error) (bool, error) {
	raw, _ := json.Marshal(item)
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:])
	metadata, _ := json.Marshal(map[string]any{"repair_key": key, "evidence": item.Evidence})
	applied := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&LeadActivityRecordModel{}).Where("lead_id = ? AND activity_type = ? AND metadata_json = ?", item.LeadID, "historical_owner_cycle_repaired", string(metadata)).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		var lead LeadModel
		if err := tx.First(&lead, item.LeadID).Error; err != nil {
			return err
		}
		if lead.Version != item.ExpectedVersion || lead.ProfileVersion != item.ExpectedProfileVersion || !sameRepairValues(repairValues(lead), item.Before) {
			return types.ErrLeadVersionConflict
		}
		verified, err := planOwnerCycleRepair(ctx, tx, lead)
		if err != nil {
			return err
		}
		if verified == nil {
			return fmt.Errorf("repair evidence no longer supports the change")
		}
		checked, _ := json.Marshal(verified)
		if string(checked) != string(raw) {
			return fmt.Errorf("repair evidence differs from approved manifest")
		}
		now := time.Now()
		updates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now}
		if item.Before.Status != item.After.Status {
			updates["status"] = item.After.Status
		}
		for _, field := range []struct {
			name          string
			before, after *time.Time
		}{
			{"current_owner_started_at", item.Before.StartedAt, item.After.StartedAt},
			{"current_owner_first_followup_at", item.Before.OwnerFirstAt, item.After.OwnerFirstAt},
			{"first_followup_at", item.Before.LifecycleFirstAt, item.After.LifecycleFirstAt},
		} {
			if (field.before == nil) != (field.after == nil) || (field.before != nil && field.after != nil && !field.before.Equal(*field.after)) {
				updates[field.name] = field.after
			}
		}
		result := tx.Model(&LeadModel{}).Where("id = ? AND version = ? AND profile_version = ?", item.LeadID, item.ExpectedVersion, item.ExpectedProfileVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return types.ErrLeadVersionConflict
		}
		before, _ := json.Marshal(item.Before)
		after, _ := json.Marshal(item.After)
		if err := tx.Create(&LeadActivityRecordModel{LeadID: item.LeadID, ActivityType: "historical_owner_cycle_repaired", ActorUserID: "maintenance:owner-cycle", ActorRole: "system", FromValueJSON: string(before), ToValueJSON: string(after), MetadataJSON: string(metadata), CreatedAt: now}).Error; err != nil {
			return err
		}
		if afterWrite != nil {
			lead.Version++
			lead.Status = item.After.Status
			lead.CurrentOwnerStartedAt = item.After.StartedAt
			lead.CurrentOwnerFirstFollowupAt = item.After.OwnerFirstAt
			lead.FirstFollowupAt = item.After.LifecycleFirstAt
			if err := afterWrite(tx, lead.ToEntity()); err != nil {
				return err
			}
		}
		applied = true
		return nil
	})
	return applied, err
}
