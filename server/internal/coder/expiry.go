// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

// RunTokenExpiry warns once per expiry date, since GitLab disables expired tokens silently and the first symptom would be a refused push.
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
