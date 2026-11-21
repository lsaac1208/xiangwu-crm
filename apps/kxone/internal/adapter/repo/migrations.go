package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SchemaMigrationModel struct {
	Version   string    `gorm:"primaryKey;type:varchar(32)"`
	Name      string    `gorm:"type:varchar(255);not null"`
	Checksum  string    `gorm:"type:varchar(64);not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (SchemaMigrationModel) TableName() string {
	return "schema_migrations"
}

type migrationFile struct {
	Version  string
	Name     string
	Path     string
	SQL      string
	Checksum string
}

func RunMigrations(db *gorm.DB, dir string) error {
	if strings.TrimSpace(dir) == "" {
		dir = "migrations"
	}
	files, err := loadMigrationFiles(dir)
	if err != nil {
		return err
	}
	if err := db.AutoMigrate(&SchemaMigrationModel{}); err != nil {
		return fmt.Errorf("prepare schema_migrations: %w", err)
	}
	var applied []SchemaMigrationModel
	if err := db.Order("version").Find(&applied).Error; err != nil {
		return err
	}
	byVersion := make(map[string]migrationFile, len(files))
	for _, file := range files {
		if _, duplicate := byVersion[file.Version]; duplicate {
			return fmt.Errorf("duplicate migration version %s", file.Version)
		}
		byVersion[file.Version] = file
	}
	latest := ""
	for _, existing := range applied {
		file, present := byVersion[existing.Version]
		// Additive newer migrations may remain after an application-only rollback.
		if !present && len(files) > 0 && existing.Version > files[len(files)-1].Version {
			continue
		}
		if !present || file.Checksum != existing.Checksum || file.Name != existing.Name {
			return fmt.Errorf("applied migration %s does not match release", existing.Version)
		}
		if existing.Version > latest {
			latest = existing.Version
		}
	}
	present := make(map[string]bool, len(applied))
	for _, existing := range applied {
		present[existing.Version] = true
	}
	for _, file := range files {
		if !present[file.Version] && file.Version < latest {
			return fmt.Errorf("out-of-order migration %s", file.Version)
		}
	}
	for _, file := range files {
		if err := applyMigration(db, file); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	files := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(content)
		files = append(files, migrationFile{
			Version:  version,
			Name:     entry.Name(),
			Path:     path,
			SQL:      string(content),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func migrationVersion(name string) (string, error) {
	parts := strings.SplitN(name, "_", 2)
	version := strings.TrimSpace(parts[0])
	if len(version) != 6 {
		return "", fmt.Errorf("invalid migration version in %q: use 6 digits like 000001", name)
	}
	for _, r := range version {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("invalid migration version in %q", name)
		}
	}
	return version, nil
}

func applyMigration(db *gorm.DB, file migrationFile) error {
	var existing SchemaMigrationModel
	err := db.Where("version = ?", file.Version).First(&existing).Error
	if err == nil {
		if existing.Checksum != file.Checksum {
			return fmt.Errorf("migration %s checksum mismatch", file.Name)
		}
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("query migration %s: %w", file.Name, err)
	}

	statements := splitSQLStatements(filterMigrationSQLForDialect(file.SQL, db.Dialector.Name()))
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if !hasExecutableSQL(statement) {
				continue
			}
			if err := tx.Exec(statement).Error; err != nil {
				if isBenignAdditiveMigrationError(statement, err) {
					continue
				}
				return fmt.Errorf("apply migration %s: %w", file.Name, err)
			}
		}
		record := &SchemaMigrationModel{
			Version:   file.Version,
			Name:      file.Name,
			Checksum:  file.Checksum,
			AppliedAt: time.Now(),
		}
		if err := tx.Create(record).Error; err != nil {
			return fmt.Errorf("record migration %s: %w", file.Name, err)
		}
		return nil
	})
}

func isBenignAdditiveMigrationError(statement string, err error) bool {
	if err == nil {
		return false
	}
	normalizedStatement := strings.ToLower(strings.TrimSpace(stripLeadingSQLComments(statement)))
	if !strings.HasPrefix(normalizedStatement, "alter table") &&
		!strings.HasPrefix(normalizedStatement, "create index") &&
		!strings.HasPrefix(normalizedStatement, "create unique index") {
		return false
	}

	message := strings.ToLower(err.Error())
	knownBenignFragments := []string{
		"duplicate column",
		"duplicate column name",
		"duplicate key name",
		"already exists",
	}
	for _, fragment := range knownBenignFragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func stripLeadingSQLComments(statement string) string {
	lines := strings.Split(statement, "\n")
	for len(lines) > 0 {
		trimmed := strings.TrimSpace(lines[0])
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			lines = lines[1:]
			continue
		}
		break
	}
	return strings.Join(lines, "\n")
}

func filterMigrationSQLForDialect(sql, dialect string) string {
	var out strings.Builder
	enabled := true
	const directive = "-- +kxone dialect "
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, directive) {
			enabled = migrationDialectMatches(strings.TrimSpace(strings.TrimPrefix(trimmed, directive)), dialect)
			continue
		}
		if enabled {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func migrationDialectMatches(spec, dialect string) bool {
	spec = strings.TrimSpace(strings.ToLower(spec))
	dialect = strings.TrimSpace(strings.ToLower(dialect))
	if spec == "" || spec == "all" || spec == "*" {
		return true
	}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == dialect || item == "all" || item == "*" {
			return true
		}
	}
	return false
}

func hasExecutableSQL(statement string) bool {
	for _, line := range strings.Split(statement, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		return true
	}
	return false
}

func splitSQLStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	var quote rune
	escaped := false
	inLineComment := false

	for i, r := range sql {
		next := rune(0)
		if i+1 < len(sql) {
			next = rune(sql[i+1])
		}
		if inLineComment {
			current.WriteRune(r)
			if r == '\n' {
				inLineComment = false
			}
			continue
		}
		if quote == 0 && r == '-' && next == '-' {
			inLineComment = true
			current.WriteRune(r)
			continue
		}
		if quote != 0 {
			current.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
			current.WriteRune(r)
			continue
		}
		if r == ';' {
			statements = append(statements, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		statements = append(statements, tail)
	}
	return statements
}
