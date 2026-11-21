package repo

import (
	"context"

	"github.com/lsaac128/kxone/pkg/types"
)

// VisitManagementLeads streams the complete cohort in one database read. Only
// reporting fields are selected; contacts are neither loaded nor decrypted.
func (r *GormLeadRepository) VisitManagementLeads(ctx context.Context, filter LeadStatsFilter, visit func(*types.Lead)) error {
	query := r.db.WithContext(ctx).Model(&LeadModel{}).Select("id, lead_no, customer_name, source_channel, status, business_stage, ownership_state, result_status, assigned_sales_user_id, owner_user_id, assigned_inviter_user_id, inviter_user_id, closer_user_id, current_owner_user_id, current_owner_role, current_appointment_id, created_at, assigned_at, claimed_at, first_followup_at, current_owner_started_at, current_owner_first_followup_at, next_followup_due_at, inviter_claimed_at, delivered_at")
	query = applyCreatedAtRange(query, r.db, filter.StartTime, filter.EndTime)
	if filter.SalesUserID != "" {
		query = applyLeadSalesScope(query, filter.SalesUserID)
	}
	rows, err := query.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var model LeadModel
		if err := r.db.ScanRows(rows, &model); err != nil {
			return err
		}
		visit(model.ToEntity())
	}
	return rows.Err()
}
