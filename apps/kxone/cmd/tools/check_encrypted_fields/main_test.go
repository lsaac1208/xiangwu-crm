package main

import (
	"database/sql"
	"testing"

	"github.com/lsaac128/kxone/internal/security"
)

func TestReadOnlyScanDetectsCorruptionAndLeavesOriginalIntact(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE leads(id INTEGER PRIMARY KEY,customer_mobile TEXT,customer_wechat TEXT)"); err != nil {
		t.Fatal(err)
	}
	p, _ := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	encoded, _ := p.Protect("enc:v1:user-text")
	if _, err := db.Exec("INSERT INTO leads VALUES(1,?,?),(2,?,?)", encoded, "wx-legacy", "enc:v1:broken-secret", ""); err != nil {
		t.Fatal(err)
	}
	result, err := scan(db, p)
	if err != nil || result.OK || len(result.Invalid) != 1 || result.Invalid[0] != "leads.customer_mobile:2" {
		t.Fatalf("scan=%+v err=%v", result, err)
	}
	var original string
	db.QueryRow("SELECT customer_mobile FROM leads WHERE id=2").Scan(&original)
	if original != "enc:v1:broken-secret" {
		t.Fatal("read-only scan altered corrupt data")
	}
}
