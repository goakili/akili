// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"context"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
)

// TokenExpiryWarning is how long before a forge token expires the admins are warned.
const TokenExpiryWarning = 14 * 24 * time.Hour

const tokenExpiryEvery = 12 * time.Hour

// RunTokenExpiry re-reads GitLab token expiry on the leader twice a day and calls warn once per
// expiry date when it is near. GitLab disables expired tokens without warning, and the first
// symptom would otherwise be a refused push.
func (s *Service) RunTokenExpiry(ctx context.Context, leading func() bool, warn func(context.Context, *models.Integration)) {
	t := time.NewTicker(tokenExpiryEvery)
	defer t.Stop()
	first := time.After(time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-t.C:
		}
		if leading() {
			s.checkTokenExpiry(ctx, time.Now(), warn)
		}
	}
}

func (s *Service) checkTokenExpiry(ctx context.Context, now time.Time, warn func(context.Context, *models.Integration)) {
	var list []models.Integration
	s.db.WithContext(ctx).Where("kind = ?", models.ForgeGitLab).Find(&list)
	for i := range list {
		it := &list[i]
		if err := s.RefreshTokenInfo(ctx, it); err != nil {
			logger.Warn("gitlab token check failed", "integration", it.ID, "error", err)
		}
		if it.TokenExpiresAt == nil || it.TokenExpiryNotified || it.TokenExpiresAt.Sub(now) > TokenExpiryWarning {
			continue
		}
		res := s.db.WithContext(ctx).Model(&models.Integration{}).Where("id = ? AND NOT token_expiry_notified", it.ID).UpdateColumn("token_expiry_notified", true)
		if res.Error == nil && res.RowsAffected == 1 {
			warn(ctx, it)
		}
	}
}
