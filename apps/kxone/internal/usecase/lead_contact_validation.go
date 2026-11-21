package usecase

import (
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"strings"
)

func validLeadContact(phone, wechat string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return strings.TrimSpace(wechat) != ""
	}
	normalized := security.NormalizeMobile(phone)
	return hasOnlySupportedPhoneCharacters(phone) && len(normalized) >= 6 && len(normalized) <= 20
}

// Every intake path shares data constraints, independently of its actor or source.
func validateLeadIntake(req *CreateLeadRequest, external bool) error {
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerWechat = strings.TrimSpace(req.CustomerWechat)
	req.City = strings.TrimSpace(req.City)
	req.ProductName = strings.TrimSpace(req.ProductName)
	req.SourceChannel = strings.TrimSpace(req.SourceChannel)
	if external && req.CustomerName == "" {
		req.CustomerName = "未留姓名"
	}
	for _, field := range []struct {
		value string
		limit int
	}{
		{req.CustomerName, 64}, {req.City, 64}, {req.ProductName, 128}, {req.CustomerWechat, 128},
		{req.SourceChannel, 128}, {req.SourceCampaign, 512}, {req.ContentPath, 512},
	} {
		if len([]rune(field.value)) > field.limit {
			return types.ErrInvalidLeadProfile
		}
	}
	if req.CustomerName == "" || !validLeadContact(req.CustomerMobile, req.CustomerWechat) {
		return types.ErrInvalidLeadProfile
	}
	req.CustomerMobile = security.NormalizeMobile(req.CustomerMobile)
	return nil
}
