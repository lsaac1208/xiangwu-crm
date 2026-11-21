package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	cfg := config.Load()
	if cfg.Security.FieldEncryptionKey == "" {
		log.Fatal("FIELD_ENCRYPTION_KEY is required")
	}
	mobileHashKey := cfg.MobileHashKeyOrFallback()
	if mobileHashKey == "" {
		log.Fatal("MOBILE_HASH_KEY or FIELD_ENCRYPTION_KEY is required")
	}

	protector, err := security.NewAESGCMFieldProtector(cfg.Security.FieldEncryptionKey)
	if err != nil {
		log.Fatalf("invalid field encryption key: %v", err)
	}
	hasher, err := security.NewHMACMobileHasher(mobileHashKey)
	if err != nil {
		log.Fatalf("invalid mobile hash key: %v", err)
	}

	db, err := repo.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dryRun, _ := strconv.ParseBool(os.Getenv("DRY_RUN"))
	var scanned, plaintextCandidates, hashCandidates, updated int64
	var leads []repo.LeadModel

	err = db.WithContext(ctx).
		Where("customer_mobile <> ''").
		FindInBatches(&leads, 200, func(tx *gorm.DB, batch int) error {
			for _, lead := range leads {
				scanned++
				plaintext := lead.CustomerMobile
				encrypted := lead.CustomerMobile
				needsEncryption := !security.IsEncryptedField(lead.CustomerMobile)
				if needsEncryption {
					plaintextCandidates++
					var err error
					encrypted, err = protector.Protect(lead.CustomerMobile)
					if err != nil {
						return err
					}
				} else {
					revealed, err := protector.Reveal(lead.CustomerMobile)
					if err != nil {
						return err
					}
					plaintext = revealed
				}
				mobileHash := hasher.HashMobile(plaintext)
				needsHash := mobileHash != "" && lead.CustomerMobileHash != mobileHash
				if needsHash {
					hashCandidates++
				}
				if !needsEncryption && !needsHash {
					continue
				}
				if dryRun {
					continue
				}
				updates := map[string]any{}
				if needsEncryption {
					updates["customer_mobile"] = encrypted
				}
				if needsHash {
					updates["customer_mobile_hash"] = mobileHash
				}
				result := db.Session(&gorm.Session{NewDB: true}).
					WithContext(ctx).
					Model(&repo.LeadModel{}).
					Where("id = ? AND customer_mobile = ?", lead.ID, lead.CustomerMobile).
					UpdateColumns(updates)
				if result.Error != nil {
					return result.Error
				}
				updated += result.RowsAffected
			}
			return nil
		}).Error
	if err != nil {
		log.Fatalf("maintain legacy mobile fields: %v", err)
	}

	log.Printf("legacy mobile maintenance done: scanned=%d plaintext_candidates=%d hash_candidates=%d updated=%d dry_run=%t", scanned, plaintextCandidates, hashCandidates, updated, dryRun)
}
