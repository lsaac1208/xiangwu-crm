package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"gorm.io/gorm"
)

func main() {
	database := flag.String("db", "", "new SQLite database path (must not already exist)")
	migrations := flag.String("migrations", "migrations", "migration directory")
	leadCount := flag.Int("leads", 10_000, "number of fixture leads")
	userCount := flag.Int("users", 500, "number of fixture users")
	flag.Parse()

	if *database == "" {
		log.Fatal("-db is required")
	}
	if *leadCount < 1 || *leadCount > 100_000 || *userCount < 1 || *userCount > 10_000 {
		log.Fatal("fixture size is outside the safe range")
	}
	absolute, err := filepath.Abs(*database)
	if err != nil {
		log.Fatalf("resolve database path: %v", err)
	}
	if _, err := os.Stat(absolute); err == nil {
		log.Fatalf("refusing to overwrite existing database: %s", absolute)
	} else if !os.IsNotExist(err) {
		log.Fatalf("inspect database path: %v", err)
	}

	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: absolute, MaxConns: 1, LogLevel: "warn"})
	if err != nil {
		log.Fatalf("open fixture database: %v", err)
	}
	if err := repo.RunMigrations(db, *migrations); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < *userCount; start += 100 {
			end := min(start+100, *userCount)
			users := make([]repo.WeComUserBindingModel, 0, end-start)
			for index := start; index < end; index++ {
				role := []string{"inviter", "closer", "manager", "dispatcher"}[index%4]
				users = append(users, repo.WeComUserBindingModel{
					CRMUserID: int64(index + 1), Username: fmt.Sprintf("fixture-%04d", index+1),
					WeComUserID: fmt.Sprintf("fixture-user-%04d", index+1), EmployeeName: fixtureName(index),
					Role: role, Status: "active", CreatedAt: now, UpdatedAt: now,
				})
			}
			if err := tx.Create(&users).Error; err != nil {
				return err
			}
		}

		statuses := []string{"pending_dispatch", "assigned_pending_claim", "claimed", "following", "public_pool", "invalid", "converted", "closed"}
		cities := []string{"襄阳市樊城区", "襄阳市襄州区", "武汉市武昌区", "十堰市"}
		products := []string{"高三艺考文化课全日制冲刺班", "复读提分规划", "高考志愿填报咨询", "服务卡"}
		for start := 0; start < *leadCount; start += 500 {
			end := min(start+500, *leadCount)
			leads := make([]repo.LeadModel, 0, end-start)
			for index := start; index < end; index++ {
				status := statuses[index%len(statuses)]
				createdAt := now.Add(-time.Duration(index%720) * time.Hour)
				lead := repo.LeadModel{
					LeadNo: fmt.Sprintf("STRESS%08d", index+1), CustomerName: fixtureName(index + *userCount),
					CustomerMobileMasked: fmt.Sprintf("13%d****%04d", index%10, index%10_000),
					CustomerWechatMasked: fmt.Sprintf("wx_%04d***", index%10_000), City: cities[index%len(cities)],
					ProductName: products[index%len(products)], SourceChannel: []string{"小红书", "抖音", "表单", "CRM人工新建"}[index%4],
					LeadLevel: []string{"high", "medium", "low"}[index%3], BusinessStage: "invitation", OwnershipState: "unassigned",
					Status: status, Version: index % 7, ProfileVersion: 1, StarRating: index%5 + 1,
					CreatedByUserID: "fixture-user-0001", CreatedByRole: "manager", CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Duration(index%180) * time.Minute),
				}
				if status != "pending_dispatch" && status != "public_pool" {
					owner := fmt.Sprintf("fixture-user-%04d", index%*userCount+1)
					lead.AssignedSalesUserID, lead.AssignedInviterUserID, lead.CurrentOwnerUserID = owner, owner, owner
					lead.CurrentOwnerRole, lead.OwnershipState = "inviter", "private"
					assignedAt := createdAt.Add(10 * time.Minute)
					lead.AssignedAt = &assignedAt
				}
				if status == "following" {
					dueAt := now.Add(time.Duration(index%96-48) * time.Hour)
					lead.NextFollowupDueAt = &dueAt
				}
				if status == "public_pool" {
					lead.OwnershipState, lead.PoolReason = "public_pool", "followup_overdue"
					enteredAt := createdAt.Add(48 * time.Hour)
					lead.PoolEnteredAt = &enteredAt
				}
				if status == "converted" {
					lead.BusinessStage, lead.ResultStatus, lead.DealAmount = "finished", "converted", float64(3000+(index%20)*500)
					convertedAt := createdAt.Add(72 * time.Hour)
					lead.ConvertedAt = &convertedAt
				}
				leads = append(leads, lead)
			}
			if err := tx.Create(&leads).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("generate fixture: %v", err)
	}

	var actualUsers, actualLeads int64
	if err := db.Model(&repo.WeComUserBindingModel{}).Count(&actualUsers).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Model(&repo.LeadModel{}).Count(&actualLeads).Error; err != nil {
		log.Fatal(err)
	}
	fmt.Printf("stress fixture ready: database=%s users=%d leads=%d\n", absolute, actualUsers, actualLeads)
}

func fixtureName(index int) string {
	surnames := []string{"张", "李", "王", "赵", "周", "孙", "陈", "刘"}
	return fmt.Sprintf("%s同学-%04d-家长长姓名兼容测试", surnames[index%len(surnames)], index+1)
}
