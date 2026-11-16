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
	"errors"
	"net/url"
	"strings"

	"github.com/goakili/akili/server/internal/models"
)

type GitRemote struct {
	Host  string // without port: the SSH and HTTPS ports of one forge differ
	Owner string
	Repo  string
	// Path is every path segment before Repo, so a nested GitLab owner (a/b/c) can match.
	Path string
}

// ParseGitRemote takes owner and repo from the last two path segments, so forges served under a path prefix work.
func ParseGitRemote(raw string) (GitRemote, error) {
	raw = strings.TrimSpace(raw)
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh" && u.Scheme != "git") {
			return GitRemote{}, errors.New("unsupported git remote")
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(raw, "@"); at >= 0 && strings.Contains(raw[at:], ":") {
		h, p, _ := strings.Cut(raw[at+1:], ":")
		host, path = h, p
	} else {
		return GitRemote{}, errors.New("unsupported git remote")
	}
	parts := strings.FieldsFunc(strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git"), func(r rune) bool { return r == '/' })
	if host == "" || len(parts) < 2 {
		return GitRemote{}, errors.New("git remote has no owner/repository")
	}
	return GitRemote{Host: strings.ToLower(host), Owner: parts[len(parts)-2], Repo: parts[len(parts)-1], Path: strings.Join(parts[:len(parts)-1], "/")}, nil
}

// ResolveRemote matches the remote path's suffix against project owner/repo, and the host when the project knows its web URL.
func (s *Service) ResolveRemote(ctx context.Context, org, remote string) (*models.Project, error) {
	r, err := ParseGitRemote(remote)
	if err != nil {
		return nil, err
	}
	var candidates []models.Project
	s.db.WithContext(ctx).Where("organization_id = ? AND lower(repo) = lower(?)", org, r.Repo).Find(&candidates)
	var best *models.Project
	for i := range candidates {
		p := &candidates[i]
		owner, path := strings.ToLower(p.Owner), strings.ToLower(r.Path)
		if path != owner && !strings.HasSuffix(path, "/"+owner) {
			continue
		}
		u, err := url.Parse(p.WebURL)
		if p.WebURL != "" && (err != nil || !strings.EqualFold(u.Hostname(), r.Host)) {
			continue
		}
		// The longest owner wins: a/b/c is a better match for a/b/c/repo than c.
		if best == nil || len(p.Owner) > len(best.Owner) {
			best = p
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}
