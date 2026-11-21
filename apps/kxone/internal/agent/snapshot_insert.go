package agent

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// snapshotInserter is used only for the already redacted private projection.
// Nullable historical fields are common, especially each absent timestamp's
// three derived columns. SQL NULL literals avoid a cgo bind for every empty
// column. Keep a bounded set of shapes; unusually varied records use the dense
// statement instead of growing an unbounded prepared-statement cache.
const snapshotInsertShapeLimit = 16

type snapshotInsertStatement struct {
	stmt     *sqlite3.SQLiteStmt
	indexes  []int
	bindings []driver.NamedValue
}

type snapshotInserter struct {
	conn    *sqlite3.SQLiteConn
	table   string
	columns int
	mask    []byte
	shapes  map[string]*snapshotInsertStatement
	dense   *snapshotInsertStatement
}

func newSnapshotInserter(conn *sqlite3.SQLiteConn, table string, columns int) *snapshotInserter {
	return &snapshotInserter{conn: conn, table: table, columns: columns, mask: make([]byte, (columns+7)/8), shapes: make(map[string]*snapshotInsertStatement)}
}

func (w *snapshotInserter) insert(ctx context.Context, values []any) error {
	if len(values) != w.columns {
		return errors.New("snapshot projection column count mismatch")
	}
	clear(w.mask)
	for i, value := range values {
		if value != nil {
			w.mask[i/8] |= 1 << (i % 8)
		}
	}
	statement := w.shapes[string(w.mask)]
	if statement == nil {
		if len(w.shapes) < snapshotInsertShapeLimit {
			var err error
			statement, err = w.prepare(values)
			if err != nil {
				return err
			}
			w.shapes[string(w.mask)] = statement
		} else {
			if w.dense == nil {
				var err error
				w.dense, err = w.prepare(nil)
				if err != nil {
					return err
				}
			}
			statement = w.dense
		}
	}
	for i, column := range statement.indexes {
		value, err := driver.DefaultParameterConverter.ConvertValue(values[column])
		if err != nil {
			return err
		}
		statement.bindings[i].Value = value
	}
	_, err := statement.stmt.ExecContext(ctx, statement.bindings)
	return err
}

func (w *snapshotInserter) prepare(values []any) (*snapshotInsertStatement, error) {
	statement := &snapshotInsertStatement{}
	placeholders := make([]string, w.columns)
	for i := range placeholders {
		if values != nil && values[i] == nil {
			placeholders[i] = "NULL"
			continue
		}
		placeholders[i] = "?"
		statement.indexes = append(statement.indexes, i)
		statement.bindings = append(statement.bindings, driver.NamedValue{Ordinal: len(statement.bindings) + 1})
	}
	stmt, err := w.conn.Prepare(`INSERT INTO "` + w.table + `" VALUES (` + strings.Join(placeholders, ",") + `)`)
	if err != nil {
		return nil, err
	}
	statement.stmt = stmt.(*sqlite3.SQLiteStmt)
	return statement, nil
}

func (w *snapshotInserter) close() {
	for _, statement := range w.shapes {
		statement.stmt.Close()
	}
	if w.dense != nil {
		w.dense.stmt.Close()
	}
}
