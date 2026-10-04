// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/goakili/akili/server/internal/models"
)

// GitRemote is a repository named by a git remote URL.
type GitRemote struct {
	Host  string // without port: the SSH and HTTPS ports of one forge differ
	Owner string
	Repo  string
}

// ParseGitRemote reads https://, ssh:// and scp-like (git@host:owner/repo) remotes. Owner and repo
// are the last two path segments, so forges served under a path prefix work too.
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
	return GitRemote{Host: strings.ToLower(host), Owner: parts[len(parts)-2], Repo: parts[len(parts)-1]}, nil
}

// ResolveRemote finds the organization's project for a git remote: same owner and repository, and,
// when the project knows its web URL, the same host.
func (s *Service) ResolveRemote(ctx context.Context, org, remote string) (*models.Project, error) {
	r, err := ParseGitRemote(remote)
	if err != nil {
		return nil, err
	}
	var candidates []models.Project
	s.db.WithContext(ctx).Where("organization_id = ? AND lower(owner) = lower(?) AND lower(repo) = lower(?)", org, r.Owner, r.Repo).
		Find(&candidates)
	for i := range candidates {
		p := &candidates[i]
		u, err := url.Parse(p.WebURL)
		if p.WebURL == "" || (err == nil && strings.EqualFold(u.Hostname(), r.Host)) {
			return p, nil
		}
	}
	return nil, ErrNotFound
}
