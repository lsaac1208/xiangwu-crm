package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MeasureScope struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Source        string `json:"source,omitempty"`
	Visibility    string `json:"visibility"`
	SelectionHash string `json:"selection_hash"`
	GroupBy       string `json:"group_by,omitempty"`
}
type MeasuredFact struct {
	ID          string    `json:"fact_id"`
	ResultID    string    `json:"result_id"`
	ScopeID     string    `json:"scope_id"`
	Level       string    `json:"level"`
	Metric      string    `json:"metric"`
	RecordTypes []string  `json:"record_types,omitempty"`
	Group       string    `json:"group,omitempty"`
	GroupLabel  string    `json:"group_label,omitempty"`
	Value       int64     `json:"value"`
	Additive    bool      `json:"additive"`
	SnapshotAt  time.Time `json:"snapshot_at"`
}
type MeasureRequest struct {
	SQL        string `json:"sql"`
	Source     string `json:"source"`
	Visibility string `json:"visibility"`
	GroupBy    string `json:"group_by"`
}

// Entity identities are derived in SQLite from the already-redacted projection.
// This avoids a second Go copy and per-record parameter allocation.
func (s *Snapshot) prepareEntityIndex(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE entity_index_history(record_type TEXT NOT NULL,record_id INTEGER NOT NULL,lead_id INTEGER,actor_user_id TEXT,business_stage TEXT,occurred_date_shanghai TEXT,source_channel TEXT,current_visible INTEGER NOT NULL,PRIMARY KEY(record_type,record_id))`); err != nil {
		return err
	}
	cols := strings.Fields("record_type record_id lead_id actor_user_id business_stage occurred_date_shanghai source_channel current_visible")
	s.columns["entity_index"], s.columns["entity_index_history"] = cols, cols
	_, err := tx.ExecContext(ctx, `CREATE VIEW entity_index AS SELECT * FROM entity_index_history WHERE current_visible=1`)
	return err
}

func (s *Snapshot) Measure(ctx context.Context, req MeasureRequest) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return analysisWork(ctx, func(ctx context.Context) (*Result, error) {
		return s.measureOnAnalysisThread(ctx, req)
	})
}

func (s *Snapshot) measureOnAnalysisThread(ctx context.Context, req MeasureRequest) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("数据快照已关闭")
	}
	if req.Visibility == "" {
		req.Visibility = "current"
	}
	if req.Visibility != "current" && req.Visibility != "history" {
		return nil, errors.New("visibility只能为current或history")
	}
	groups := map[string]bool{"actor_user_id": true, "record_type": true, "business_stage": true, "occurred_date_shanghai": true, "source_channel": true}
	if req.GroupBy != "" && !groups[req.GroupBy] {
		return nil, errors.New("分组维度不在可信实体字典中")
	}
	kind := "query_set"
	selection := req.SQL
	if req.Source != "" {
		if req.SQL != "" {
			return nil, errors.New("source与sql只能选择一个")
		}
		var spec *snapshotTable
		for i := range snapshotTables {
			if snapshotTables[i].name == req.Source {
				spec = &snapshotTables[i]
				break
			}
		}
		if spec == nil {
			return nil, errors.New("source不是允许的业务原表")
		}
		name := spec.name
		if req.Visibility == "history" {
			name = physicalTable(*spec)
		}
		selection = `SELECT '` + spec.name + `' AS record_type,id AS record_id FROM "` + name + `"`
		kind = "whole_source"
	}
	selection, _ = s.Sanitize(selection)
	selection, err := oneReadStatement(selection)
	if err != nil {
		return nil, err
	}
	index := "entity_index"
	if req.Visibility == "history" {
		index = "entity_index_history"
	}
	ctx, releaseQuery, err := s.enterQuery(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseQuery()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	// Verify identities against server-owned mappings; never trust lead_id or
	// actor aliases supplied by the selection SQL. Unknown identities are errors.
	check := `WITH requested AS (` + selection + `) SELECT COUNT(*) FROM requested r LEFT JOIN ` + index + ` e ON e.record_type=r.record_type AND e.record_id=r.record_id WHERE e.record_id IS NULL OR typeof(r.record_id)!='integer' OR r.record_id<=0 OR r.record_type IS NULL`
	var invalid int64
	err = conn.QueryRowContext(ctx, check).Scan(&invalid)
	conn.Close()
	if err != nil {
		return nil, fmt.Errorf("集合必须返回record_type和整数record_id且仅含允许的只读数据: %w", err)
	}
	if invalid > 0 {
		return nil, fmt.Errorf("集合含%d条非法、未知或当前范围不可见记录，未执行统计", invalid)
	}
	followupTable := "lead_followups"
	if req.Visibility == "history" {
		followupTable = "lead_followups_history"
	}
	// A record set defines membership, never the meaning of a metric. This
	// additional business metric is computed from the approved original field,
	// not a SQL alias or the model's description of a follow-up as a plan.
	// Materialize the trusted mapping and its plan flag once. Otherwise SQLite
	// can inline matched into both aggregation branches, repeating the entity
	// join and the full historical-plan source scan for global/group results.
	planFlag := `CASE WHEN e.record_type='lead_followups' AND e.record_id IN (SELECT id FROM ` + followupTable + ` WHERE NULLIF(next_followup_at,'') IS NOT NULL) THEN 1 ELSE 0 END`
	base := `WITH requested AS (` + selection + `), keyed AS (SELECT DISTINCT record_type,record_id FROM requested), matched AS MATERIALIZED (SELECT e.*,` + planFlag + ` AS has_historical_followup_plan FROM keyed r JOIN ` + index + ` e ON e.record_type=r.record_type AND e.record_id=r.record_id) `
	planCount := `COALESCE(SUM(has_historical_followup_plan),0)`
	query := base + `SELECT 'global' AS scope_level,'' AS group_value,COUNT(*) AS distinct_records,COUNT(DISTINCT lead_id) AS distinct_leads,COALESCE(SUM(CASE WHEN lead_id IS NULL THEN 1 ELSE 0 END),0) AS records_without_lead,` + planCount + ` AS historical_followup_plan_records,GROUP_CONCAT(DISTINCT record_type) AS record_types FROM matched`
	if req.GroupBy != "" {
		query += ` UNION ALL SELECT 'group',COALESCE("` + req.GroupBy + `",''),COUNT(*),COUNT(DISTINCT lead_id),SUM(CASE WHEN lead_id IS NULL THEN 1 ELSE 0 END),` + planCount + `,GROUP_CONCAT(DISTINCT record_type) FROM matched GROUP BY "` + req.GroupBy + `"`
	}
	result, err := s.queryLockedWithLimits(ctx, query, nil, 100000, 0, 5*time.Second, 100000, 50<<20, "")
	if err != nil {
		return nil, err
	}
	if result.Truncated {
		return nil, errors.New("分组过多，完整统计未保存；请缩小分组范围")
	}
	hash := sha256.Sum256([]byte(selection))
	digest := hex.EncodeToString(hash[:])
	scopeHash := sha256.Sum256([]byte(s.At.Format(time.RFC3339Nano) + "\n" + req.Visibility + "\n" + digest))
	result.MeasureScope = &MeasureScope{ID: hex.EncodeToString(scopeHash[:]), Kind: kind, Source: req.Source, Visibility: req.Visibility, SelectionHash: digest, GroupBy: req.GroupBy}
	employees := map[string][]string{}
	if req.GroupBy == "actor_user_id" {
		rows, err := s.db.QueryContext(ctx, `SELECT wecom_userid,employee_name FROM crm_wecom_user_bindings`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name sql.NullString
			if err = rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			employees[id.String] = append(employees[id.String], name.String)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, row := range result.Rows {
		recordTypes := []string{}
		if value := textValue(row[6]); value != "" {
			recordTypes = strings.Split(value, ",")
			sort.Strings(recordTypes)
		} else if req.Source != "" {
			recordTypes = []string{req.Source}
		}
		label := textValue(row[1])
		if label == "" {
			label = "未知"
		}
		if names := employees[textValue(row[1])]; len(names) == 1 && names[0] != "" {
			label = names[0]
		} else if len(names) > 1 {
			label += "（姓名存在多个匹配，需核对）"
		}
		for i, metric := range []string{"distinct_records", "distinct_leads", "records_without_lead", "historical_followup_plan_records"} {
			if metric == "historical_followup_plan_records" && !containsRecordType(recordTypes, "lead_followups") {
				continue
			}
			value := integerValue(row[i+2])
			result.Facts = append(result.Facts, MeasuredFact{ID: uuid.NewString(), ResultID: result.ID, ScopeID: result.MeasureScope.ID, Level: fmt.Sprint(row[0]), Metric: metric, RecordTypes: append([]string{}, recordTypes...), Group: fmt.Sprint(row[1]), GroupLabel: label, Value: value, Additive: metric != "distinct_leads", SnapshotAt: s.At})
		}
	}
	result.MaterializedRows = len(result.Rows)
	result.MaterializedComplete = true
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		return nil, encodeErr
	}
	if len(encoded) > 50<<20 {
		return nil, errors.New("完整统计及事实超过50MB容量，未发布部分结果；请缩小分组范围")
	}
	return result, nil
}

func containsRecordType(types []string, wanted string) bool {
	for _, name := range types {
		if name == wanted {
			return true
		}
	}
	return false
}
