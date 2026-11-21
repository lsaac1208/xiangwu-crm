package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
)

// prodcopy-v1 replays a server-local online backup of the production SQLite
// file inside the isolated harness. The copy never leaves the server, is
// deleted after the run, and nothing derived from customer text is written to
// evidence: only the forbidden-fragment guard holds known names in memory.
const liveProdCopyDataset = "prodcopy-v1"

const prodCopyUserIDOffset = 100000

func prodCopyPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("KXONE_AGENT_LIVE_PRODCOPY_DB")
	if path == "" || !filepath.IsAbs(path) || !strings.HasPrefix(path, "/var/tmp/") {
		t.Fatal("KXONE_AGENT_LIVE_PRODCOPY_DB must be an absolute server-local path under /var/tmp")
	}
	return path
}

func prodCopySpecific(value string) bool {
	han, alnum := 0, 0
	for _, ch := range value {
		if unicode.Is(unicode.Han, ch) {
			han++
		}
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			alnum++
		}
	}
	return han >= 2 || alnum >= 6
}

func loadLiveProdCopyFixture(t *testing.T) (liveFixture, string) {
	t.Helper()
	path := prodCopyPath(t)
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=ro&_query_only=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT COALESCE(customer_name,''),COALESCE(customer_wechat,''),COALESCE(customer_mobile,'') FROM leads`)
	if err != nil {
		t.Fatal("production copy unreadable")
	}
	defer rows.Close()
	protector, err := security.NewAESGCMFieldProtector(liveFieldKey(t))
	if err != nil {
		t.Fatal("invalid field encryption key")
	}
	seen := map[string]bool{}
	forbidden := []any{}
	for rows.Next() {
		var name, wechat, mobile string
		if rows.Scan(&name, &wechat, &mobile) != nil {
			t.Fatal("production copy unreadable")
		}
		for _, v := range []string{strings.TrimSpace(name), strings.TrimSpace(wechat), strings.TrimSpace(mobile)} {
			if security.IsEncryptedField(v) {
				// Decrypt in memory so the outbound guard also blocks the
				// plaintext contact; it is never logged or written.
				plain, err := protector.Reveal(v)
				if err != nil {
					t.Fatal("production copy contact cannot be decrypted with the server key")
				}
				v = strings.TrimSpace(plain)
			}
			if v == "" || !prodCopySpecific(v) || seen[v] {
				continue
			}
			seen[v] = true
			forbidden = append(forbidden, v)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", path, info.Size(), len(forbidden))))
	return liveFixture{Version: liveProdCopyDataset, Dataset: liveProdCopyDataset, ReferenceNow: time.Now().UTC().Format(time.RFC3339), Tables: map[string][]map[string]any{}, IndependentExpected: map[string]any{"forbidden_provider_payload_fragments": forbidden}}, hex.EncodeToString(digest[:])
}

// importLiveProdCopy copies every business table of the production copy into
// the isolated router database (shared columns only). Accounts keep their
// textual IDs but get an ID offset so the two isolated evaluation accounts
// cannot collide with real administrators.
func importLiveProdCopy(t *testing.T, db *gorm.DB, adminID int64) {
	t.Helper()
	path := prodCopyPath(t)
	err := db.Connection(func(tx *gorm.DB) error {
		if err := tx.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
			return err
		}
		if err := tx.Exec("ATTACH DATABASE ? AS src", path).Error; err != nil {
			return err
		}
		defer tx.Exec("DETACH DATABASE src")
		var tables []string
		if err := tx.Raw(`SELECT name FROM src.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'ai_%' ORDER BY name`).Scan(&tables).Error; err != nil {
			return err
		}
		copied := 0
		for _, table := range tables {
			var mainCols, srcCols []struct{ Name string }
			if table == "schema_migrations" {
				continue
			}
			tx.Raw(`SELECT name FROM pragma_table_info(?, 'main')`, table).Scan(&mainCols)
			if len(mainCols) == 0 {
				continue
			}
			tx.Raw(`SELECT name FROM pragma_table_info(?, 'src')`, table).Scan(&srcCols)
			have := map[string]bool{}
			for _, c := range srcCols {
				have[c.Name] = true
			}
			cols, exprs := []string{}, []string{}
			for _, c := range mainCols {
				if !have[c.Name] {
					continue
				}
				expr := `"` + c.Name + `"`
				if table == "crm_wecom_user_bindings" && (c.Name == "id" || c.Name == "crm_user_id") {
					expr = fmt.Sprintf(`"%s"+%d`, c.Name, prodCopyUserIDOffset)
				}
				cols = append(cols, `"`+c.Name+`"`)
				exprs = append(exprs, expr)
			}
			if err := tx.Exec(`DELETE FROM main."` + table + `"`).Error; err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
			if err := tx.Exec(`INSERT INTO main."` + table + `" (` + strings.Join(cols, ",") + `) SELECT ` + strings.Join(exprs, ",") + ` FROM src."` + table + `"`).Error; err != nil {
				return fmt.Errorf("copy %s: %w", table, err)
			}
			copied++
		}
		var mainLeads, srcLeads int64
		tx.Raw(`SELECT COUNT(*) FROM main.leads`).Scan(&mainLeads)
		tx.Raw(`SELECT COUNT(*) FROM src.leads`).Scan(&srcLeads)
		if mainLeads != srcLeads || srcLeads == 0 {
			return fmt.Errorf("lead copy incomplete: %d of %d", mainLeads, srcLeads)
		}
		t.Logf("production copy imported: %d tables, %d leads", copied, srcLeads)
		return nil
	})
	if err != nil {
		t.Fatalf("import production copy: %v", err)
	}
	reference := time.Now()
	for index, role := range []string{"admin", "manager"} {
		account := repo.WeComUserBindingModel{ID: adminID + int64(index), CRMUserID: adminID + int64(index), Username: fmt.Sprintf("live-evaluation-%s", role), WeComUserID: fmt.Sprintf("live-evaluation-%s", role), Role: role, Status: "active", AuthVersion: 1, AccountVersion: 1, EmployeeName: "隔离验收账号", CreatedAt: reference, UpdatedAt: reference}
		if err := db.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// liveFieldKey is the server's own field key from the process environment
// (loaded from the production env file by the start script); never printed.
func liveFieldKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("FIELD_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("PII_ENCRYPTION_KEY")
	}
	if key == "" {
		t.Fatal("FIELD_ENCRYPTION_KEY is required for the production copy replay")
	}
	return key
}
