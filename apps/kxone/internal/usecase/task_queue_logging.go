package usecase

import "log"

func logNonBlockingError(action string, leadID int64, err error) {
	if err == nil {
		return
	}
	log.Printf("%s失败: lead_id=%d err=%v", action, leadID, err)
}
