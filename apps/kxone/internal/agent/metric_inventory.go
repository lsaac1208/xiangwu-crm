package agent

func buildOwnerInventory(p MetricParams) ([]metricQuery, error) {
	f, err := leadFilters(p, "l.created_at_date_shanghai", "")
	if err != nil {
		return nil, err
	}
	owned := `COALESCE(l.current_owner_user_id,'')<>'' AND COALESCE(l.ownership_state,'') NOT IN ('public_pool','assigned_pending_claim','pending_assignment') AND l.status NOT IN ('public_pool','assigned_pending_claim','pending_dispatch')`
	pending := `(l.ownership_state='assigned_pending_claim' OR l.status='assigned_pending_claim')`
	args := []any{}
	if p.OwnerID != "" {
		owned += ` AND l.current_owner_user_id=?`
		pending += ` AND COALESCE(NULLIF(l.assigned_inviter_user_id,''),l.assigned_sales_user_id)=?`
		args = append(args, p.OwnerID, p.OwnerID)
	}
	base := ` FROM leads l WHERE ` + f.clause()
	summaryArgs := append(append([]any{}, args...), f.args...)
	queries := []metricQuery{{title: "汇总", sql: `SELECT COALESCE(SUM(CASE WHEN ` + owned + ` THEN 1 ELSE 0 END),0) AS "名下线索数",COALESCE(SUM(CASE WHEN ` + pending + ` THEN 1 ELSE 0 END),0) AS "另有待领取"` + base, args: summaryArgs}}
	statusArgs := append([]any{}, f.args...)
	if p.OwnerID != "" {
		statusArgs = append(statusArgs, p.OwnerID)
	}
	queries = append(queries, metricQuery{title: "名下状态拆分", sql: `SELECT ` + statusLabelSQL + ` AS "状态",COUNT(*) AS "线索数"` + base + ` AND (` + owned + `) GROUP BY l.status ORDER BY l.status`, args: statusArgs})
	return queries, nil
}
