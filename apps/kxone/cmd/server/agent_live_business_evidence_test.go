package main

// Test-only immutable-business evidence. Only digests and counts are exported;
// raw customer/contact/credential values never leave the hashing process.
import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var liveBusinessFingerprintTables = []string{
	"leads", "lead_followups", "lead_dispatch_records", "lead_appointments", "lead_appointment_events", "closer_availability",
	"lead_pool_records", "lead_activity_records", "lead_reminder_records", "crm_wecom_user_bindings", "channel_integrations",
	"channel_integration_audits", "group_robot_notifications", "wecom_card_tasks", "wecom_callback_logs", "lead_deletion_events", "xiangwu_form_submissions",
}

type liveBusinessTableFingerprint struct {
	Exists bool   `json:"exists"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

type liveBusinessFingerprint struct {
	FormatVersion     string                                  `json:"format_version"`
	CapturedAt        time.Time                               `json:"captured_at"`
	SHA256            string                                  `json:"sha256"`
	Tables            map[string]liveBusinessTableFingerprint `json:"tables"`
	ExcludedTables    []string                                `json:"excluded_tables"`
	MissingTables     []string                                `json:"missing_tables"`
	Scope             string                                  `json:"scope"`
	FullDatabaseProof bool                                    `json:"full_database_proof"`
}

type liveBusinessIntegrity struct {
	Before            liveBusinessFingerprint `json:"before"`
	After             liveBusinessFingerprint `json:"after"`
	Consistent        bool                    `json:"consistent"`
	ChangedTables     []string                `json:"changed_tables"`
	FullDatabaseProof bool                    `json:"full_database_proof"`
}

func validateLiveBusinessFingerprintPath(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("isolated fixture database path unavailable")
	}
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return errors.New("temporary fixture root unavailable")
	}
	relative, err := filepath.Rel(temporary, resolved)
	if err != nil || filepath.Base(resolved) != "kxone-test.db" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("business fingerprint accepts only the temporary test-router database")
	}
	return nil
}

func captureLiveBusinessFingerprint(ctx context.Context, fixture *gorm.DB) (liveBusinessFingerprint, error) {
	result := liveBusinessFingerprint{FormatVersion: "live-business-fingerprint-v1", CapturedAt: time.Now().UTC(), Tables: map[string]liveBusinessTableFingerprint{}, ExcludedTables: []string{}, MissingTables: []string{}, Scope: "all columns of existing tables in the explicit 17 analysis-business allowlist; missing tables recorded as absent; excluded tables are not covered", FullDatabaseProof: false}
	if fixture == nil {
		return result, errors.New("isolated fixture database required")
	}
	var files []struct {
		Name string
		File string
	}
	if err := fixture.Raw("PRAGMA database_list").Scan(&files).Error; err != nil {
		return result, errors.New("fixture database identity unavailable")
	}
	var path string
	for _, file := range files {
		if file.Name == "main" {
			path = file.File
		}
	}
	if err := validateLiveBusinessFingerprintPath(path); err != nil {
		return result, err
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&_query_only=1&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", uri)
	if err != nil {
		return result, errors.New("read-only fixture connection unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, errors.New("read-only fingerprint transaction unavailable")
	}
	defer tx.Rollback()
	allowed := map[string]bool{}
	for _, table := range liveBusinessFingerprintTables {
		allowed[table] = true
	}
	names, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return result, errors.New("table coverage inventory unavailable")
	}
	existing := map[string]bool{}
	for names.Next() {
		var name string
		if err := names.Scan(&name); err != nil {
			names.Close()
			return result, errors.New("table inventory unreadable")
		}
		if !allowed[name] {
			result.ExcludedTables = append(result.ExcludedTables, name)
		}
		existing[name] = true
	}
	if err := names.Err(); err != nil {
		names.Close()
		return result, errors.New("table inventory incomplete")
	}
	names.Close()
	for _, table := range liveBusinessFingerprintTables {
		if !existing[table] {
			result.Tables[table] = liveBusinessTableFingerprint{Exists: false}
			result.MissingTables = append(result.MissingTables, table)
			continue
		}
		fingerprint, err := fingerprintLiveBusinessTable(ctx, tx, table)
		if err != nil {
			return result, fmt.Errorf("business fingerprint %s: %w", table, err)
		}
		result.Tables[table] = fingerprint
	}
	if err := tx.Commit(); err != nil {
		return result, errors.New("read-only fingerprint transaction failed")
	}
	encoded, err := json.Marshal(struct {
		Version string                                  `json:"version"`
		Tables  map[string]liveBusinessTableFingerprint `json:"tables"`
	}{result.FormatVersion, result.Tables})
	if err != nil {
		return result, errors.New("fingerprint summary encoding failed")
	}
	sum := sha256.Sum256(encoded)
	result.SHA256 = hex.EncodeToString(sum[:])
	return result, nil
}

func fingerprintLiveBusinessTable(ctx context.Context, tx *sql.Tx, table string) (liveBusinessTableFingerprint, error) {
	type column struct {
		ID      int
		Name    string
		Type    string
		NotNull int
		Default any
		PK      int
	}
	quoted := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+quoted(table)+")")
	if err != nil {
		return liveBusinessTableFingerprint{}, errors.New("schema unavailable")
	}
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.NotNull, &c.Default, &c.PK); err != nil {
			rows.Close()
			return liveBusinessTableFingerprint{}, errors.New("schema unreadable")
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return liveBusinessTableFingerprint{}, errors.New("schema incomplete")
	}
	rows.Close()
	var primary []column
	for _, c := range columns {
		if c.PK > 0 {
			primary = append(primary, c)
		}
	}
	if len(columns) == 0 || len(primary) == 0 {
		return liveBusinessTableFingerprint{}, errors.New("explicit stable primary key required")
	}
	sort.Slice(primary, func(i, j int) bool { return primary[i].PK < primary[j].PK })
	order := []string{}
	for _, c := range primary {
		order = append(order, quoted(c.Name))
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode(columns); err != nil {
		return liveBusinessTableFingerprint{}, errors.New("schema encoding failed")
	}
	data, err := tx.QueryContext(ctx, "SELECT * FROM "+quoted(table)+" ORDER BY "+strings.Join(order, ","))
	if err != nil {
		return liveBusinessTableFingerprint{}, errors.New("rows unavailable")
	}
	defer data.Close()
	var count int64
	for data.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := data.Scan(pointers...); err != nil {
			return liveBusinessTableFingerprint{}, errors.New("row unreadable")
		}
		canonical := make([][2]string, len(values))
		for i, value := range values {
			switch v := value.(type) {
			case nil:
				canonical[i] = [2]string{"null", ""}
			case int64:
				canonical[i] = [2]string{"integer", strconv.FormatInt(v, 10)}
			case float64:
				canonical[i] = [2]string{"real", strconv.FormatFloat(v, 'g', -1, 64)}
			case bool:
				canonical[i] = [2]string{"boolean", strconv.FormatBool(v)}
			case string:
				canonical[i] = [2]string{"text", v}
			case []byte:
				canonical[i] = [2]string{"blob", base64.StdEncoding.EncodeToString(v)}
			case time.Time:
				canonical[i] = [2]string{"time", v.UTC().Format("2006-01-02T15:04:05.000000000Z")}
			default:
				return liveBusinessTableFingerprint{}, errors.New("unsupported canonical value type")
			}
		}
		if err := encoder.Encode(canonical); err != nil {
			return liveBusinessTableFingerprint{}, errors.New("row encoding failed")
		}
		count++
	}
	if err := data.Err(); err != nil {
		return liveBusinessTableFingerprint{}, errors.New("rows incomplete")
	}
	return liveBusinessTableFingerprint{Exists: true, Rows: count, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func compareLiveBusinessFingerprints(before, after liveBusinessFingerprint) liveBusinessIntegrity {
	result := liveBusinessIntegrity{Before: before, After: after, ChangedTables: []string{}, Consistent: before.FormatVersion == after.FormatVersion && before.SHA256 != "" && before.SHA256 == after.SHA256, FullDatabaseProof: false}
	all := map[string]bool{}
	for table := range before.Tables {
		all[table] = true
	}
	for table := range after.Tables {
		all[table] = true
	}
	for table := range all {
		if before.Tables[table] != after.Tables[table] {
			result.ChangedTables = append(result.ChangedTables, table)
		}
	}
	sort.Strings(result.ChangedTables)
	return result
}
