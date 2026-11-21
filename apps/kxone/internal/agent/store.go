package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

var ErrForbidden = errors.New("forbidden")
var ErrConflict = errors.New("agent_conflict")
var ErrUnavailable = errors.New("agent_unavailable")

type Conversation struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	AccountID int64     `gorm:"index;not null" json:"-"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
}

func (Conversation) TableName() string { return "ai_conversations" }

type Message struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	ConversationID string    `gorm:"index;not null" json:"conversation_id"`
	RunID          string    `gorm:"index" json:"run_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	BlocksJSON     string    `json:"-"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func (Message) TableName() string { return "ai_messages" }
func (m Message) DTO() map[string]any {
	var blocks []Block
	_ = json.Unmarshal([]byte(m.BlocksJSON), &blocks)
	if blocks == nil {
		blocks = []Block{}
	}
	return map[string]any{"id": m.ID, "conversation_id": m.ConversationID, "run_id": m.RunID, "role": m.Role, "content": m.Content, "blocks": blocks, "status": m.Status, "created_at": m.CreatedAt}
}

type Run struct {
	ID                  string     `gorm:"primaryKey;size:64" json:"id"`
	ConversationID      string     `gorm:"index;not null" json:"conversation_id"`
	AccountID           int64      `gorm:"index;uniqueIndex:idx_ai_request,priority:1" json:"-"`
	RequestID           string     `gorm:"uniqueIndex:idx_ai_request,priority:2;size:128" json:"-"`
	AuthVersion         int64      `json:"-"`
	Status              string     `gorm:"index" json:"status"`
	Model               string     `json:"model"`
	RuleVersion         int64      `json:"rule_version"`
	Rules               string     `json:"-"`
	Error               string     `json:"error,omitempty"`
	ErrorCode           string     `json:"error_code,omitempty"`
	AnalysisContextJSON string     `json:"-"`
	SnapshotAt          *time.Time `json:"snapshot_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

func (Run) TableName() string { return "ai_runs" }

type ResultRecord struct {
	ID        string `gorm:"primaryKey;size:64"`
	RunID     string `gorm:"index;not null"`
	Data      string
	CreatedAt time.Time
}

func (ResultRecord) TableName() string { return "ai_results" }

type Event struct {
	Seq       int64     `gorm:"primaryKey;autoIncrement" json:"seq"`
	RunID     string    `gorm:"index;not null" json:"run_id"`
	Type      string    `json:"type"`
	Data      string    `json:"-"`
	CreatedAt time.Time `json:"-"`
}

func (Event) TableName() string { return "ai_events" }

type Settings struct {
	ID               int64     `gorm:"primaryKey" json:"-"`
	Enabled          bool      `json:"enabled"`
	BudgetMicro      int64     `json:"budget_micro"`
	BaseURL          string    `json:"base_url"`
	Model            string    `json:"model"`
	KeyCiphertext    string    `json:"-"`
	FallbackJSON     string    `gorm:"not null;default:'{}'" json:"-"`
	InputPriceMicro  int64     `json:"-"`
	OutputPriceMicro int64     `json:"-"`
	PriceVersion     string    `json:"price_version"`
	UpdatedBy        int64     `json:"updated_by"`
	Version          int64     `json:"version"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (Settings) TableName() string { return "ai_settings" }

type Preference struct {
	AccountID int64     `gorm:"primaryKey" json:"-"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Preference) TableName() string { return "ai_preferences" }

type Rule struct {
	ID           int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID    int64      `json:"-"`
	Content      string     `json:"content"`
	Status       string     `gorm:"index" json:"status"`
	Version      int64      `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	PublishedBy  int64      `json:"published_by,omitempty"`
	SourceRuleID int64      `json:"source_rule_id,omitempty"`
}

func (Rule) TableName() string { return "ai_rules" }

type ExportRecord struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	RunID     string    `gorm:"index" json:"run_id"`
	Format    string    `json:"format"`
	Path      string    `json:"-"`
	Hash      string    `json:"sha256"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (ExportRecord) TableName() string { return "ai_exports" }

// MigrateForTest is for isolated fixtures; production uses checksummed SQL migrations.
func MigrateForTest(db *gorm.DB) error {
	return db.AutoMigrate(&Conversation{}, &Message{}, &Run{}, &ResultRecord{}, &Event{}, &Settings{}, &Preference{}, &Rule{}, &ExportRecord{}, &BudgetReservation{})
}

func (s *Service) ownedConversation(ctx context.Context, id string, actor Actor) (*Conversation, error) {
	if err := s.validate(ctx, actor); err != nil {
		return nil, err
	}
	var row Conversation
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, actor.AccountID).First(&row).Error; err != nil {
		return nil, ErrForbidden
	}
	return &row, nil
}
func (s *Service) ownedRun(ctx context.Context, id string, actor Actor) (*Run, error) {
	if err := s.validate(ctx, actor); err != nil {
		return nil, err
	}
	var row Run
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, actor.AccountID).First(&row).Error; err != nil {
		return nil, ErrForbidden
	}
	return &row, nil
}
func (s *Service) event(runID, kind string, data any) error {
	return appendRunEvent(s.db, runID, kind, data)
}

func appendRunEvent(db *gorm.DB, runID, kind string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return db.Create(&Event{RunID: runID, Type: kind, Data: string(b), CreatedAt: time.Now().UTC()}).Error
}

// finishRun commits the durable answer and both terminal events together. A
// reader must never observe a terminal run whose done event is not yet stored.
func (s *Service) finishRun(ctx context.Context, runID, status, errorMessage, content string, blocks []Block) error {
	raw, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Run
		if err := tx.First(&current, "id = ?", runID).Error; err != nil {
			return err
		}
		// Retrying an uncertain commit must not duplicate final events.
		if current.Status != "running" && current.Status != "queued" {
			return nil
		}
		updated := tx.Model(&Message{}).Where("run_id = ? AND role = ?", runID, "assistant").Updates(map[string]any{"content": content, "blocks_json": string(raw), "status": status})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("terminal assistant message missing or duplicated")
		}
		updates := map[string]any{"status": status, "error": errorMessage, "error_code": runErrorCode(errorMessage), "finished_at": time.Now().UTC()}
		if status == "completed" {
			for _, b := range blocks {
				if b.AnalysisContext != nil {
					encoded, err := json.Marshal(b.AnalysisContext)
					if err != nil {
						return err
					}
					updates["analysis_context_json"] = string(encoded)
				}
			}
		}
		if err := tx.Model(&Run{}).Where("id = ?", runID).Updates(updates).Error; err != nil {
			return err
		}
		var message Message
		if err := tx.Where("run_id = ? AND role = ?", runID, "assistant").First(&message).Error; err != nil {
			return err
		}
		if err := appendRunEvent(tx, runID, "message", map[string]any{"message": message.DTO()}); err != nil {
			return err
		}
		return appendRunEvent(tx, runID, "done", map[string]any{"status": status, "error": errorMessage})
	})
}
func (s *Service) results(ctx context.Context, runID string) ([]Result, error) {
	var rows []ResultRecord
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("created_at,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []Result{}
	for _, row := range rows {
		var r Result
		if err := json.Unmarshal([]byte(row.Data), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *Service) saveResult(ctx context.Context, runID string, r *Result) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.ReturnedRows = len(r.Rows)
	r.MaterializedRows = len(r.Rows)
	r.MaterializedComplete = !r.Truncated
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(&ResultRecord{ID: r.ID, RunID: runID, Data: string(b), CreatedAt: time.Now().UTC()}).Error
}
