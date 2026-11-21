package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/usecase"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	database := flag.String("db", "", "SQLite database path")
	output := flag.String("plan", "", "write evidence manifest, read only")
	apply := flag.String("apply", "", "apply a previously reviewed manifest")
	flag.Parse()
	if *database == "" || (*output == "") == (*apply == "") {
		return fmt.Errorf("provide -db and exactly one of -plan or -apply")
	}
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: *database, MaxConns: 1, LogLevel: "silent"})
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	ctx := context.Background()
	if *output != "" {
		plan, err := repo.PlanOwnerCycleRepairs(ctx, db)
		if err != nil {
			return err
		}
		raw, _ := json.MarshalIndent(plan, "", "  ")
		if err = os.WriteFile(*output, raw, 0600); err != nil {
			return err
		}
		fmt.Printf("planned=%d without_evidence=%d manifest=%s\n", len(plan.Items), plan.WithoutEvidence, *output)
		return nil
	}
	raw, err := os.ReadFile(*apply)
	if err != nil {
		return err
	}
	var plan repo.OwnerCycleRepairPlan
	if err = json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	applied, skipped, conflicts := 0, 0, 0
	for _, item := range plan.Items {
		changed, err := usecase.ApplyOwnerCycleRepair(ctx, db, item)
		if err != nil {
			conflicts++
			fmt.Printf("lead_id=%d skipped=%v\n", item.LeadID, err)
			continue
		}
		if changed {
			applied++
		} else {
			skipped++
		}
	}
	fmt.Printf("applied=%d already_applied=%d conflicts=%d\n", applied, skipped, conflicts)
	if conflicts > 0 {
		return fmt.Errorf("some changes were withheld; regenerate and review the evidence manifest")
	}
	return nil
}
