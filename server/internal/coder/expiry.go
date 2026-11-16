// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
