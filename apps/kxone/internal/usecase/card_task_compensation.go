package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func recordFailedCardSend(
	ctx context.Context,
	cardRepo repo.CardTaskRepository,
	leadID int64,
	receiverUserID string,
	scene types.CardScene,
	err error,
) {
	if cardRepo == nil || err == nil || leadID <= 0 || receiverUserID == "" {
		return
	}

	now := time.Now()
	task := &types.WeComCardTask{
		LeadID:         leadID,
		CardScene:      scene,
		ReceiverUserID: receiverUserID,
		TaskID:         fmt.Sprintf("enqueue_failed_%d_%s_%d", leadID, scene, now.UnixNano()),
		CardStatus:     types.CardStatusUpdateFailed,
		LastError:      fmt.Sprintf("任务入队失败: %v", err),
		SentAt:         now,
	}
	if createErr := cardRepo.Create(ctx, task); createErr != nil {
		logNonBlockingError("记录待补偿卡片发送任务", leadID, createErr)
	}
}

func recordFailedCardUpdate(
	ctx context.Context,
	cardRepo repo.CardTaskRepository,
	leadID int64,
	sourceScene types.CardScene,
	targetScene types.CardScene,
	err error,
) {
	if cardRepo == nil || err == nil || leadID <= 0 {
		return
	}

	task, getErr := cardRepo.GetLatestByScene(ctx, leadID, sourceScene)
	if getErr != nil {
		logNonBlockingError("查询待补偿卡片更新任务", leadID, getErr)
		return
	}
	if task == nil {
		return
	}

	now := time.Now()
	task.CardScene = targetScene
	task.CardStatus = types.CardStatusUpdateFailed
	task.LastError = fmt.Sprintf("任务入队失败: %v", err)
	task.UpdatedAt = &now
	task.NextRetryAt = nil
	if updateErr := cardRepo.Update(ctx, task); updateErr != nil {
		logNonBlockingError("记录待补偿卡片更新任务", leadID, updateErr)
	}
}
