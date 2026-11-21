package agent

import (
	"context"
	"database/sql"
	"runtime/trace"
	"strings"
)

// indexProjectedTable never reads the source database or original customer text.
// Both indexes are built from the approved physical projection, in small SQL
// batches. Keeping the derivation inside SQLite avoids per-record Go maps and
// duplicate bind arrays, which otherwise make foreground requests share a large
// amount of allocation and garbage-collection work with each analysis.
func (s *Snapshot) indexProjectedTable(ctx context.Context, tx *sql.Tx, spec snapshotTable, pacer *snapshotPacer) error {
	region := trace.StartRegion(ctx, "agent.snapshot.entity_text_index")
	defer region.End()
	trace.Log(ctx, "agent.snapshot.table", spec.name)
	physical := physicalTable(spec)
	if spec.name == "leads" {
		// Later tables join this exact snapshot to establish current visibility.
		if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX leads_id ON leads_history(id)`); err != nil {
			return err
		}
	}
	col := func(name string) string {
		if !hasField(spec, name) {
			return "NULL"
		}
		return `p."` + name + `"`
	}
	first := func(names ...string) string {
		terms := make([]string, 0, len(names)+1)
		for _, name := range names {
			if hasField(spec, name) {
				terms = append(terms, `NULLIF(`+col(name)+`, '')`)
			}
		}
		terms = append(terms, `''`)
		if len(terms) == 1 {
			return terms[0]
		}
		return `COALESCE(` + strings.Join(terms, ",") + `)`
	}
	actor := projectedActorSQL(spec, col)
	leadID, ref, mappedLead, source, visible := "0", "NULL", "NULL", "''", "1"
	from := ` FROM "` + physical + `" p`
	if spec.name == "leads" {
		leadID, ref, mappedLead = `p.id`, `p.customer_ref`, `p.id`
		source, visible = `COALESCE(p.source_channel,'')`, `(p.deleted_at IS NULL)`
	} else if hasField(spec, "lead_id") {
		leadID, ref = `COALESCE(p.lead_id,0)`, `p.customer_ref`
		mappedLead, source = `l.id`, `COALESCE(l.source_channel,'')`
		visible = `(l.id IS NOT NULL AND l.deleted_at IS NULL)`
		from += ` LEFT JOIN leads_history l ON l.id=p.lead_id`
	}
	// Invalid/absent timestamps are already NULL in the physical projection.
	// Use the stored date columns, never reinterpret UTC as local time.
	occurred := func(suffix string) string {
		var candidates []string
		for _, name := range []string{"created_at", "occurred_at"} {
			if hasField(spec, name) {
				candidates = append(candidates, `p."`+name+suffix+`"`)
			}
		}
		if len(candidates) == 0 {
			return "NULL"
		}
		if len(candidates) == 1 {
			return candidates[0]
		}
		return `COALESCE(` + strings.Join(candidates, ",") + `)`
	}
	entityQuery := `INSERT INTO entity_index_history SELECT '` + spec.name + `',p.id,` + mappedLead + `,` + actor + `,` + col("business_stage") + `,` + occurred("_date_shanghai") + `,` + source + `,` + visible + from + ` WHERE p.rowid>? AND p.rowid<=?`
	var textParts []string
	for _, field := range spec.fields {
		if field.kind == "text" || field.kind == "json" || field.kind == "dimension" {
			textParts = append(textParts, `CASE WHEN COALESCE(`+col(field.name)+`,'')!='' THEN char(10)||'`+field.name+`: '||`+col(field.name)+` ELSE '' END`)
		}
	}
	var recordQuery string
	if spec.name != "crm_wecom_user_bindings" && len(textParts) > 0 {
		text := `substr((` + strings.Join(textParts, `||`) + `),2)`
		event := first("event_type", "activity_type", "action_type", "action", "reminder_type", "card_scene")
		if spec.name == "wecom_callback_logs" {
			event = first("event_key")
		}
		recordQuery = `INSERT INTO analysis_records_history SELECT '` + spec.name + `:'||p.id,p.id,'` + spec.name + `',` + leadID + `,` + ref + `,` + actor + `,` + col("business_stage") + `,` + event + `,` + occurred("") + `,` + occurred("_shanghai") + `,` + occurred("_date_shanghai") + `,` + text + `,p.excluded_fragments FROM "` + physical + `" p WHERE p.rowid>? AND p.rowid<=? AND (` + text + `)!=''`
	}
	const batchSize = int64(128)
	for start := int64(0); start < s.counts[spec.name]; start += batchSize {
		if err := pacer.checkpoint(ctx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, entityQuery, start, start+batchSize); err != nil {
			return err
		}
		if recordQuery != "" {
			if _, err := tx.ExecContext(ctx, recordQuery, start, start+batchSize); err != nil {
				return err
			}
		}
	}
	return nil
}

// Operators are evidenced by table-specific fields. Participants, recipients
// and current owners cannot substitute for a missing historical operator.
func projectedActorSQL(spec snapshotTable, col func(string) string) string {
	var field string
	switch spec.name {
	case "leads":
		field = "created_by_user_id"
	case "lead_followups":
		field = "sales_user_id"
	case "lead_dispatch_records":
		return `CASE WHEN p.action_type IN ('assign','reassign','transfer','self_assign','self_create') THEN COALESCE(p.manager_user_id,'') WHEN p.action_type='claim' THEN COALESCE(p.sales_user_id,'') ELSE '' END`
	case "lead_appointment_events", "lead_pool_records", "lead_activity_records", "channel_integration_audits":
		field = "actor_user_id"
	case "closer_availability", "channel_integrations":
		field = "created_by"
	case "wecom_callback_logs":
		field = "from_user_id"
	case "lead_deletion_events":
		field = "actor_userid"
	default:
		return `''`
	}
	return `COALESCE(` + col(field) + `,'')`
}
