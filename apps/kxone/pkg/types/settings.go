package types

import "time"

// SystemSetting 系统配置项。
type SystemSetting struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Value       string    `json:"value,omitempty"`
	ValueType   string    `json:"value_type"`
	Group       string    `json:"group"`
	Description string    `json:"description,omitempty"`
	IsSensitive bool      `json:"is_sensitive"`
	UpdatedBy   string    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
