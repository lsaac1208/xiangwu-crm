package agent

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestSnapshotSparseInsertMatchesDenseValuesAcrossShapeLimit(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const columns = 71 // More than one machine word of nullability bits.
	var names, predicates []string
	for i := 0; i < columns; i++ {
		name := fmt.Sprintf("c%d", i)
		names = append(names, name)
		predicates = append(predicates, fmt.Sprintf("s.%s IS d.%s AND typeof(s.%s)=typeof(d.%s)", name, name, name, name))
	}
	for _, table := range []string{"sparse", "dense"} {
		if _, err := conn.ExecContext(t.Context(), `CREATE TABLE "`+table+`" (`+strings.Join(names, ",")+`)`); err != nil {
			t.Fatal(err)
		}
	}
	rows := make([][]any, 256)
	for row := range rows {
		values := make([]any, columns)
		for column := range values {
			if row&(1<<(column%8)) == 0 {
				continue
			}
			// Preserve SQL NULL versus empty text, zero, bool and zero-length blob;
			// do not infer nullability from Go zero values or truthiness.
			values[column] = []any{"", int64(0), false, float64(2.75), []byte{}, "客户#23", int(7), "含引号'及换行\n"}[column%8]
		}
		rows[row] = values
	}
	if err := conn.Raw(func(raw any) error {
		writer := newSnapshotInserter(raw.(*sqlite3.SQLiteConn), "sparse", columns)
		defer writer.close()
		for _, values := range rows {
			if err := writer.insert(t.Context(), values); err != nil {
				return err
			}
		}
		if len(writer.shapes) != snapshotInsertShapeLimit || writer.dense == nil {
			return fmt.Errorf("unbounded or unexercised fallback: shapes=%d dense=%v", len(writer.shapes), writer.dense != nil)
		}
		// Revisit earlier cached shapes after the dense fallback was used. No
		// bindings from a previous row may survive and replace an explicit NULL.
		for _, row := range []int{1, 0, 15, 255} {
			if err := writer.insert(t.Context(), rows[row]); err != nil {
				return err
			}
			rows = append(rows, rows[row])
		}
		if err := writer.insert(t.Context(), []any{1}); err == nil {
			return fmt.Errorf("invalid column count accepted")
		}
		bad := make([]any, columns)
		bad[0] = map[string]string{"unsupported": "value"}
		if err := writer.insert(t.Context(), bad); err == nil {
			return fmt.Errorf("unsupported bound value accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	query := `INSERT INTO dense VALUES (` + strings.TrimRight(strings.Repeat("?,", columns), ",") + `)`
	for _, row := range rows {
		if _, err := conn.ExecContext(t.Context(), query, row...); err != nil {
			t.Fatal(err)
		}
	}
	var count, differences int
	if err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM sparse`).Scan(&count); err != nil || count != len(rows) {
		t.Fatalf("row count=%d want=%d err=%v", count, len(rows), err)
	}
	if err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM sparse s JOIN dense d ON s.rowid=d.rowid WHERE NOT (`+strings.Join(predicates, " AND ")+`)`).Scan(&differences); err != nil || differences != 0 {
		t.Fatalf("projection values or storage classes changed: differences=%d err=%v", differences, err)
	}
}
