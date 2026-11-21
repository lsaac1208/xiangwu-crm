package usecase

import (
	"context"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

func ApplyOwnerCycleRepair(ctx context.Context, db *gorm.DB, item repo.OwnerCycleRepairItem) (bool, error) {
	return repo.ApplyOwnerCycleRepair(ctx, db, item, func(tx *gorm.DB, lead *types.Lead) error {
		settings := runtime.NewDatabaseRuntimeSettings(repo.NewGormSystemSettingRepository(tx), 0)
		return scheduleFollowupReminders(ctx, repo.NewGormLeadReminderRepository(tx), snapshotFollowupPolicy(ctx, settings), lead)
	})
}
