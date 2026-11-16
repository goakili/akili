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
