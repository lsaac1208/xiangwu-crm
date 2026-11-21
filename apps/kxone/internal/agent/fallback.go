package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/security"
)

// This storage-only profile contains ciphertext; HTTP responses use its DTO.
type fallbackProfile struct {
	Enabled          bool   `json:"enabled"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	KeyCiphertext    string `json:"key_ciphertext"`
	InputPriceMicro  int64  `json:"input_price_micro"`
	OutputPriceMicro int64  `json:"output_price_micro"`
	PriceVersion     string `json:"price_version"`
}

type fallbackInput struct {
	Enabled        bool     `json:"enabled"`
	BaseURL        string   `json:"base_url"`
	Model          string   `json:"model"`
	APIKey         string   `json:"api_key"`
	ClearAPIKey    bool     `json:"clear_api_key"`
	InputPriceCNY  *float64 `json:"input_price_cny"`
	OutputPriceCNY *float64 `json:"output_price_cny"`
}

func readFallbackProfile(raw string) (fallbackProfile, error) {
	var p fallbackProfile
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	if json.Unmarshal([]byte(raw), &p) != nil {
		return p, errors.New("备用模型配置读取失败")
	}
	return p, nil
}

func (p fallbackProfile) dto(admin bool) map[string]any {
	d := map[string]any{"enabled": p.Enabled, "model": p.Model, "key_configured": p.KeyCiphertext != "", "configured": p.KeyCiphertext != "" && p.BaseURL != "" && p.Model != ""}
	if admin {
		d["base_url"] = p.BaseURL
		d["input_price_cny"] = float64(p.InputPriceMicro) / 1e6
		d["output_price_cny"] = float64(p.OutputPriceMicro) / 1e6
		d["price_version"] = p.PriceVersion
	}
	return d
}

func (s *Service) prepareFallback(ctx context.Context, current fallbackProfile, input fallbackInput) (fallbackProfile, error) {
	p := current
	p.Enabled = input.Enabled
	p.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	p.Model = strings.TrimSpace(input.Model)
	if len(input.APIKey) > 4096 || len(p.Model) > 128 {
		return p, errors.New("请检查备用模型设置")
	}
	if (p.BaseURL != current.BaseURL || p.Model != current.Model) && (input.InputPriceCNY == nil || input.OutputPriceCNY == nil) {
		return p, errors.New("更换备用模型或URL时请提交输入、输出单价")
	}
	if p.BaseURL != "" {
		if err := validateEndpoint(ctx, p.BaseURL, s.allowLocal); err != nil {
			return p, err
		}
	}
	if input.ClearAPIKey {
		p.KeyCiphertext = ""
	} else if key := strings.TrimSpace(input.APIKey); key != "" {
		if s.protector == nil {
			return p, errors.New("密钥加密服务未配置")
		}
		cipher, err := s.protector.Protect(key)
		if err != nil || !security.IsEncryptedField(cipher) {
			return p, errors.New("备用密钥加密失败")
		}
		p.KeyCiphertext = cipher
	}
	if input.InputPriceCNY != nil || input.OutputPriceCNY != nil {
		if input.InputPriceCNY == nil || input.OutputPriceCNY == nil || *input.InputPriceCNY <= 0 || *input.OutputPriceCNY <= 0 || *input.InputPriceCNY > 10000 || *input.OutputPriceCNY > 10000 {
			return p, errors.New("请填写有效的备用输入、输出单价")
		}
		p.InputPriceMicro = int64(*input.InputPriceCNY*1e6 + 0.5)
		p.OutputPriceMicro = int64(*input.OutputPriceCNY*1e6 + 0.5)
		if p.InputPriceMicro != current.InputPriceMicro || p.OutputPriceMicro != current.OutputPriceMicro || p.Model != current.Model || p.BaseURL != current.BaseURL || p.PriceVersion == "" {
			p.PriceVersion = time.Now().UTC().Format(time.RFC3339)
		}
	}
	if p.Enabled && (p.BaseURL == "" || p.Model == "" || p.KeyCiphertext == "" || p.InputPriceMicro <= 0 || p.OutputPriceMicro <= 0) {
		return p, errors.New("启用备用模型前须补齐URL、模型、Key及单价")
	}
	return p, nil
}

func (s *Service) applyFallback(cfg Config, p fallbackProfile) (Config, error) {
	cfg.Fallback = nil
	if !p.Enabled {
		return cfg, nil
	}
	if s.protector == nil || !security.IsEncryptedField(p.KeyCiphertext) || p.BaseURL == "" || p.Model == "" || p.InputPriceMicro <= 0 || p.OutputPriceMicro <= 0 || p.PriceVersion == "" {
		return cfg, ErrUnavailable
	}
	key, err := s.protector.Reveal(p.KeyCiphertext)
	if err != nil || key == "" {
		return cfg, ErrUnavailable
	}
	backup := cfg
	backup.APIKey = key
	backup.BaseURL = p.BaseURL
	backup.Model = p.Model
	backup.InputPriceMicro = p.InputPriceMicro
	backup.OutputPriceMicro = p.OutputPriceMicro
	backup.PriceVersion = p.PriceVersion
	cfg.Fallback = &backup
	return cfg, nil
}

type providerFailure struct {
	status  int
	kind    string
	phase   string
	reason  string
	message string
}

func (e *providerFailure) Error() string { return e.message }
func canFallback(err error) bool {
	var p *providerFailure
	return errors.As(err, &p) && p.kind != "context" && (p.status == 429 || p.status >= 500 || p.kind == "transport" || p.kind == "upstream")
}
