// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/forge"
	"github.com/goakili/akili/server/internal/models"
)

// RunRemote executes a remote (control-plane side) tool that has already been authorised.
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) proto.RemoteResult {
	out, err := s.runRemote(ctx, sess, tool, input)
	if err != nil {
		return proto.RemoteResult{Output: "error: " + err.Error(), IsError: true}
	}
	return proto.RemoteResult{Output: out}
}

func (s *Service) runRemote(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) (string, error) {
	if sess.ProjectID == nil || sess.Branch == "" {
		return "", ErrNoProject
	}
	p, f, err := s.projectForge(ctx, sess.OrganizationID, *sess.ProjectID)
	if err != nil {
		return "", err
	}
	switch tool {
	case proto.ToolPROpen:
		var in proto.PROpenInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return s.openPR(ctx, sess, p, f, in)
	case proto.ToolPRStatus:
		return s.prStatus(ctx, sess, p, f)
	}
	return "", fmt.Errorf("%s is not a remote tool", tool)
}

func (s *Service) openPR(ctx context.Context, sess *models.ChatSession, p *models.Project, f forge.Forge, in proto.PROpenInput) (string, error) {
	pr, err := f.FindOpenPR(ctx, p.Owner, p.Repo, sess.Branch)
	existing := err == nil
	if err != nil && !errors.Is(err, forge.ErrNotFound) {
		return "", err
	}
	if !existing {
		body := strings.TrimSpace(in.Body) + "\n\n---\n" + s.footer(ctx, sess)
		pr, err = f.CreatePR(ctx, p.Owner, p.Repo, sess.Branch, p.DefaultBranch, in.Title, body, in.Draft)
		if err != nil {
			var apiErr *forge.APIError
			if errors.As(err, &apiErr) && (apiErr.Status == 404 || apiErr.Status == 422 || apiErr.Status == 409) {
				return "", fmt.Errorf("the forge refused the pull request (%s). Push the branch first with git_push, and make sure it has commits the default branch does not", apiErr.Message)
			}
			return "", err
		}
	}
	if sess.TaskID != nil {
		s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", *sess.TaskID).Updates(map[string]any{"pr_number": pr.Number, "pr_url": pr.URL})
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID, Action: "pr.open",
		TargetType: "project", TargetID: p.ID, Metadata: map[string]any{"pr": pr.Number, "url": pr.URL, "branch": sess.Branch, "existing": existing}})
	verb := "Opened"
	if existing {
		verb = "A pull request is already open:"
	}
	return fmt.Sprintf("%s PR #%d %q (%s → %s)\n%s", verb, pr.Number, pr.Title, pr.Head, pr.Base, pr.URL), nil
}

func (s *Service) footer(ctx context.Context, sess *models.ChatSession) string {
	var agent models.Agent
	s.db.WithContext(ctx).Select("name").First(&agent, "id = ?", sess.AgentID)
	kind, id := "session", sess.ID
	if sess.TaskID != nil {
		kind, id = "task", *sess.TaskID
	}
	by := ""
	if u := s.requester(ctx, sess); u != nil && u.ForgeLogin != "" {
		by = " · requested by @" + u.ForgeLogin
	}
	// The marker carries only IDs, so webhooks can match the PR to its session even after the
	// visible text is edited; agent names are free text and could close the comment.
	return fmt.Sprintf("Opened by [Akili](https://goakili.dev) agent **%s**%s · %s `%s`\n<!-- akili:%s=%s agent=%s -->",
		agent.Name, by, kind, id, kind, id, sess.AgentID)
}

func (s *Service) prStatus(ctx context.Context, sess *models.ChatSession, p *models.Project, f forge.Forge) (string, error) {
	var b strings.Builder
	pr, err := f.FindOpenPR(ctx, p.Owner, p.Repo, sess.Branch)
	switch {
	case err == nil:
		fmt.Fprintf(&b, "PR #%d %q is %s (%s → %s)\n%s\n", pr.Number, pr.Title, pr.State, pr.Head, pr.Base, pr.URL)
	case errors.Is(err, forge.ErrNotFound):
		b.WriteString("No open pull request for this branch yet.\n")
	default:
		return "", err
	}
	st, err := f.CommitStatus(ctx, p.Owner, p.Repo, sess.Branch)
	if errors.Is(err, forge.ErrNotFound) {
		b.WriteString("The branch has not been pushed yet.\n")
		return b.String(), nil
	}
	if err != nil {
		return "", err
	}
	if st.State == "none" {
		b.WriteString("CI: no checks reported for the branch head")
		if st.SHA != "" {
			fmt.Fprintf(&b, " (%.12s)", st.SHA)
		}
		b.WriteString(". If the repository has CI, it may not have started yet.\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "CI: %s", st.State)
	if st.SHA != "" {
		fmt.Fprintf(&b, " at %.12s", st.SHA)
	}
	b.WriteString("\n")
	for _, c := range st.Checks {
		fmt.Fprintf(&b, "- %s: %s", c.Name, c.State)
		if c.Description != "" {
			fmt.Fprintf(&b, " (%s)", c.Description)
		}
		if c.URL != "" {
			fmt.Fprintf(&b, " %s", c.URL)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// TaskDiff returns the pull request diff of a coding task.
func (s *Service) TaskDiff(ctx context.Context, t *models.Task) (string, error) {
	if t.ProjectID == nil || t.PRNumber == 0 {
		return "", errors.New("this task has no pull request yet")
	}
	p, f, err := s.projectForge(ctx, t.OrganizationID, *t.ProjectID)
	if err != nil {
		return "", err
	}
	return f.PRDiff(ctx, p.Owner, p.Repo, t.PRNumber)
}
