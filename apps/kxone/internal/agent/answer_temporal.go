package agent

import (
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// This conservative check supplies a computable path for relative time claims.
// It is not a general proof of all narrative semantics. In particular, do not
// exempt an entire sentence merely because a different clause says "不代表".
var unboundTemporalClaim = regexp.MustCompile(`(?i)(?:快照|数据时点|当前时间|现在|今天|今日)(?:的)?(?:时间|日期|时点)?(?:之)?(?:前|后)|(?:早于|晚于|先于|后于).{0,12}(?:快照|数据时点|当前时间|现在|今天|今日)|(?:预约|到访|回访)(?:的)?(?:计划)?(?:时间|日期|时刻).{0,8}(?:还没到|尚未到|未到|未来|将来|已过|已经过去)|(?:预约|到访|回访).{0,10}(?:安排在|还在|仍在|是在|在|处于)(?:未来|将来)|(?:before|after).{0,12}(?:snapshot|current time)|(?:snapshot|current time).{0,12}(?:before|after)`)
var strictCalendarDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
var strictAbsoluteTime = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})$`)

func compareCellToSnapshot(value any, snapshot time.Time) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" || snapshot.IsZero() {
		return "", errors.New("时间值或快照时间缺失，不能比较；请保留未知，不推断先后")
	}
	anchor := snapshot.In(types.BusinessLocation)
	if strictCalendarDate.MatchString(text) {
		parsed, err := time.ParseInLocation("2006-01-02", text, types.BusinessLocation)
		if err != nil || parsed.Format("2006-01-02") != text {
			return "", errors.New("日期单元格不是有效上海日期，不能比较")
		}
		day := anchor.Format("2006-01-02")
		relation := "与快照同一上海日期；单元格只有日期，无法判断具体时刻先后"
		if text < day {
			relation = "早于快照的上海日期"
		} else if text > day {
			relation = "晚于快照的上海日期"
		}
		return "**服务端时间核对：" + relation + "**（单元格按上海日期解释；本轮快照上海时间=" + anchor.Format(time.RFC3339Nano) + "；不据此推断业务状态）", nil
	}
	if !strictAbsoluteTime.MatchString(text) {
		return "", errors.New("时间比较须为带Z或UTC偏移的RFC3339时间，或YYYY-MM-DD上海日期；不猜测无时区时间")
	}
	if text[len(text)-1] != 'Z' {
		hours, _ := strconv.Atoi(text[len(text)-5 : len(text)-3])
		minutes, _ := strconv.Atoi(text[len(text)-2:])
		if hours > 23 || minutes > 59 {
			return "", errors.New("时间单元格的UTC偏移无效，不能比较")
		}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return "", errors.New("时间单元格不是有效绝对时间，不能比较")
	}
	relation := "与本次快照为同一时刻"
	if parsed.Before(snapshot) {
		relation = "早于本次快照时间"
	} else if parsed.After(snapshot) {
		relation = "晚于本次快照时间"
	}
	return "**服务端时间核对：" + relation + "**（单元格上海时间=" + parsed.In(types.BusinessLocation).Format(time.RFC3339Nano) + "；本轮快照上海时间=" + anchor.Format(time.RFC3339Nano) + "；不据此推断业务状态）", nil
}
