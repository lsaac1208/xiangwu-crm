package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
)

func main() {
	driver := flag.String("driver", "sqlite", "database driver")
	database := flag.String("db", "", "database path or database name")
	migrations := flag.String("migrations", "migrations", "migration directory")
	flag.Parse()

	if *database == "" {
		log.Fatal("-db is required")
	}
	if *driver == "sqlite" {
		absolute, err := filepath.Abs(*database)
		if err != nil {
			log.Fatalf("resolve database path: %v", err)
		}
		*database = absolute
	}

	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: *driver, Database: *database, MaxConns: 1, LogLevel: "warn",
	})
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := repo.RunMigrations(db, *migrations); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	fmt.Printf("migrations applied successfully: driver=%s database=%s\n", *driver, *database)
}
