// check_encrypted_fields is a read-only deployment gate. It never emits stored
// values, customer fields, keys, or decryption error contents.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"

	"github.com/lsaac128/kxone/internal/security"
	_ "github.com/mattn/go-sqlite3"
)

type scanResult struct {
	Scanned int      `json:"scanned"`
	Invalid []string `json:"invalid_records"`
	OK      bool     `json:"ok"`
}

func scan(db *sql.DB, p security.FieldProtector) (scanResult, error) {
	result := scanResult{Invalid: []string{}, OK: true}
	queries := []struct {
		table, column, where string
		fallback             bool
	}{{"leads", "customer_mobile", "1=1", false}, {"leads", "customer_wechat", "1=1", false}, {"channel_integrations", "token_ciphertext", "1=1", false}, {"system_settings", "setting_value", "is_sensitive=1", false}, {"ai_settings", "key_ciphertext", "1=1", false}, {"ai_settings", "fallback_json", "1=1", true}}
	for _, q := range queries {
		var present int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", q.table).Scan(&present); err != nil {
			return result, fmt.Errorf("database metadata unavailable")
		}
		if present == 0 {
			continue
		}
		rows, err := db.Query("SELECT id," + q.column + " FROM " + q.table + " WHERE " + q.where)
		if err != nil {
			return result, fmt.Errorf("field scan unavailable: %s.%s", q.table, q.column)
		}
		for rows.Next() {
			var id int64
			var value sql.NullString
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return result, fmt.Errorf("field scan unavailable: %s.%s", q.table, q.column)
			}
			result.Scanned++
			stored := value.String
			invalid := false
			if q.fallback && stored != "" {
				var profile struct {
					KeyCiphertext string `json:"key_ciphertext"`
				}
				invalid = json.Unmarshal([]byte(stored), &profile) != nil
				stored = profile.KeyCiphertext
			}
			if _, err := p.Reveal(stored); err != nil {
				invalid = true
			}
			if invalid {
				result.Invalid = append(result.Invalid, fmt.Sprintf("%s.%s:%d", q.table, q.column, id))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, fmt.Errorf("field scan unavailable: %s.%s", q.table, q.column)
		}
	}
	result.OK = len(result.Invalid) == 0
	return result, nil
}
func main() {
	path := flag.String("db", "", "SQLite database path")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "-db required")
		os.Exit(1)
	}
	p, err := security.NewAESGCMFieldProtector(os.Getenv("FIELD_ENCRYPTION_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "valid FIELD_ENCRYPTION_KEY required")
		os.Exit(1)
	}
	uri := url.URL{Scheme: "file", Path: *path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		os.Exit(1)
	}
	defer db.Close()
	result, err := scan(db, p)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(result)
	if !result.OK {
		os.Exit(1)
	}
}
