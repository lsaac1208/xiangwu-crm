package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type ChannelIntegrationsUseCase struct {
	repo          repo.ChannelIntegrationRepository
	publicBaseURL string
}

func NewChannelIntegrationsUseCase(repository repo.ChannelIntegrationRepository, publicBaseURL string) *ChannelIntegrationsUseCase {
	return &ChannelIntegrationsUseCase{repo: repository, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}
}

type SaveChannelIntegrationRequest struct {
	ID          int64  `json:"-"`
	Platform    string `json:"platform"`
	AccountName string `json:"account_name"`
	Enabled     *bool  `json:"enabled"`
	Token       string `json:"token"`
	ActorUserID string `json:"-"`
}

type ChannelIntegrationsResponse struct {
	Integrations  []*types.ChannelIntegration `json:"integrations"`
	PublicBaseURL string                      `json:"public_base_url"`
	FixedEntries  []map[string]string         `json:"fixed_entries"`
}

func (uc *ChannelIntegrationsUseCase) List(ctx context.Context) (*ChannelIntegrationsResponse, error) {
	items, err := uc.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return &ChannelIntegrationsResponse{Integrations: items, PublicBaseURL: uc.publicBaseURL, FixedEntries: []map[string]string{
		{"key": "manual", "name": "手动录入", "url": uc.publicBaseURL + "/manual/"},
		{"key": "form", "name": "微信公众号表单", "url": uc.publicBaseURL + "/form/"},
	}}, nil
}

func (uc *ChannelIntegrationsUseCase) Create(ctx context.Context, req *SaveChannelIntegrationRequest) (*types.ChannelIntegration, error) {
	platform, account, err := normalizeChannelInput(req.Platform, req.AccountName)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.Token)
	if platform == types.LeadPlatformXiaohongshu && token == "" {
		return nil, fmt.Errorf("xiaohongshu token is required")
	}
	if platform != types.LeadPlatformXiaohongshu && platform != types.LeadPlatformDouyin {
		token = ""
	}
	now := time.Now()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	secretConfigured := token != ""
	item := &types.ChannelIntegration{Platform: platform, AccountName: account, CallbackPath: callbackPath(platform), AuthMode: authMode(platform, secretConfigured), SecretConfigured: secretConfigured, WebhookToken: token, Enabled: enabled, CreatedBy: req.ActorUserID, UpdatedBy: req.ActorUserID, CreatedAt: now, UpdatedAt: now}
	if err := uc.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (uc *ChannelIntegrationsUseCase) Update(ctx context.Context, req *SaveChannelIntegrationRequest) (*types.ChannelIntegration, error) {
	existing, err := uc.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, gorm.ErrRecordNotFound
	}
	platform, account, err := normalizeChannelInput(req.Platform, req.AccountName)
	if err != nil {
		return nil, err
	}
	action := "update"
	if req.Enabled != nil && existing.Enabled != *req.Enabled {
		action = "enable"
		if !*req.Enabled {
			action = "disable"
		}
		existing.Enabled = *req.Enabled
	}
	existing.Platform, existing.AccountName = platform, account
	if platform != types.LeadPlatformXiaohongshu && platform != types.LeadPlatformDouyin {
		existing.WebhookToken = ""
	} else if token := strings.TrimSpace(req.Token); token != "" {
		existing.WebhookToken = token
	}
	existing.SecretConfigured = existing.WebhookToken != ""
	existing.CallbackPath, existing.AuthMode = callbackPath(platform), authMode(platform, existing.SecretConfigured)
	existing.UpdatedBy, existing.UpdatedAt = req.ActorUserID, time.Now()
	if err := uc.repo.Update(ctx, existing, action); err != nil {
		return nil, err
	}
	return existing, nil
}

func (uc *ChannelIntegrationsUseCase) Delete(ctx context.Context, id int64, actor string) error {
	if id <= 0 {
		return gorm.ErrRecordNotFound
	}
	return uc.repo.Delete(ctx, id, strings.TrimSpace(actor))
}

func (uc *ChannelIntegrationsUseCase) CallbackURL(item *types.ChannelIntegration) string {
	return uc.publicBaseURL + item.CallbackPath + "?account=" + url.QueryEscape(item.AccountName)
}

func normalizeChannelInput(platform, account string) (string, string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	account = strings.TrimSpace(account)
	if !types.IsLeadPlatform(platform) {
		return "", "", fmt.Errorf("invalid platform")
	}
	if account == "" || len([]rune(account)) > 128 {
		return "", "", fmt.Errorf("account name is required and must not exceed 128 characters")
	}
	return platform, account, nil
}

func callbackPath(platform string) string { return "/api/leads/" + platform }
func authMode(platform string, secretConfigured bool) string {
	if platform == types.LeadPlatformXiaohongshu {
		return "hmac_sha1"
	}
	if platform == types.LeadPlatformDouyin && secretConfigured {
		return "feiyu_signature"
	}
	return "platform_compatible"
}

func IsChannelConflict(err error) bool { return errors.Is(err, repo.ErrChannelIntegrationConflict) }
