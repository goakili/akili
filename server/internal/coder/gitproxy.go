// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/gitid"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
)

// The git proxy is the only way an agent reaches a repository. It serves git smart-HTTP for one
// session: the session must be open, belong to the calling agent (identity is the tunnel) and be
// bound to a project. It adds the forge credentials, which never leave the control plane, and it
// inspects every push, refusing anything but updates to the session's own akili/* branch made of
// commits that carry the agent's git identity.

const maxPushCommands = 64 << 10

var zeroSHA = strings.Repeat("0", 40)

// GitProxy handles GitProxyPath requests for an agent.
func (s *Service) GitProxy(agentID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, proto.GitProxyPath)
		sessionID, gitPath, ok := strings.Cut(rest, "/")
		if !ok {
			http.Error(w, "bad git path", http.StatusBadRequest)
			return
		}
		// Clients use a URL ending in repo.git; only the smart-HTTP endpoints are served.
		gitPath = strings.TrimPrefix(gitPath, "repo.git/")
		service := ""
		switch {
		case gitPath == "info/refs" && r.Method == http.MethodGet:
			service = r.URL.Query().Get("service")
			if service != "git-upload-pack" && service != "git-receive-pack" {
				http.Error(w, "only smart HTTP is supported", http.StatusForbidden)
				return
			}
		case (gitPath == "git-upload-pack" || gitPath == "git-receive-pack") && r.Method == http.MethodPost:
			service = gitPath
		default:
			http.Error(w, "not a git smart-HTTP endpoint", http.StatusNotFound)
			return
		}
		ctx := r.Context()
		sess, proj, fg, err := s.sessionRepo(ctx, agentID, sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		body := io.Reader(r.Body)
		var refs []string
		var pack *verifiedPack
		meta := map[string]any{}
		if gitPath == "git-receive-pack" {
			if r.Header.Get("Content-Encoding") == "gzip" {
				zr, err := gzip.NewReader(r.Body)
				if err != nil {
					http.Error(w, "bad gzip body", http.StatusBadRequest)
					return
				}
				defer zr.Close()
				body = zr
				r.Header.Del("Content-Encoding")
			}
			prefix, packHead, cmds, err := readPushCommands(body)
			if err == nil && len(cmds) == 0 && isProbe(packHead, body) {
				// git sends a bare flush first when a push exceeds http.postBuffer; it changes nothing.
				body = bytes.NewReader(prefix)
			} else {
				if err == nil {
					err = checkPush(cmds, sess.Branch)
				}
				for _, c := range cmds {
					refs = append(refs, c.Ref)
				}
				ident, idErr := s.agentIdentity(ctx, sess.OrganizationID, agentID)
				if err == nil {
					err = idErr
				}
				meta = map[string]any{"project_id": proj.ID, "repo": proj.FullName(), "refs": refs, "branch": sess.Branch, "git_identity": ident.String()}
				if err != nil {
					s.refusePush(w, r, sess, agentID, meta, err)
					return
				}
				// A push is on the record before it reaches the forge.
				if err := s.audit.Record(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: agentID,
					Action: "git.push", TargetType: "session", TargetID: sess.ID, Metadata: meta}); err != nil {
					writeReceivePackError(w, "audit trail unavailable")
					return
				}
				pack = newVerifiedPack(io.MultiReader(bytes.NewReader(packHead), body), identityCheck(ident))
				defer pack.Close()
				body = io.MultiReader(bytes.NewReader(prefix), pack)
			}
		}

		upstream := fg.GitURL(proj.Owner, proj.Repo) + "/" + gitPath
		if r.URL.RawQuery != "" {
			upstream += "?service=" + service
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, upstream, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for _, h := range []string{"Content-Type", "Accept", "Git-Protocol", "Content-Encoding"} {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}
		user, pass, err := fg.GitAuth(ctx)
		if err != nil {
			http.Error(w, "forge credentials unavailable", http.StatusBadGateway)
			return
		}
		req.SetBasicAuth(user, pass)
		req.Header.Set("User-Agent", "git/akili-proxy")
		resp, err := s.gitHTTP.Do(req)
		if pack != nil {
			if perr := pack.Refused(); perr != nil {
				if err == nil {
					resp.Body.Close()
				}
				s.refusePush(w, r, sess, agentID, meta, perr)
				return
			}
		}
		if err != nil {
			http.Error(w, "forge unreachable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Cache-Control", "Expires", "Pragma", "Content-Encoding"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		var copyErr error
		if service == "git-receive-pack" && gitPath == "info/refs" && resp.StatusCode == http.StatusOK && resp.Header.Get("Content-Encoding") == "" {
			copyErr = advertiseNoThin(flushWriter{w}, resp.Body)
		} else {
			_, copyErr = io.Copy(flushWriter{w}, resp.Body)
		}
		if copyErr != nil {
			logger.Debug("git proxy copy ended", "error", copyErr)
		}
	})
}

func (s *Service) agentIdentity(ctx context.Context, org, agentID string) (gitid.Identity, error) {
	var a models.Agent
	if err := s.db.WithContext(ctx).First(&a, "id = ? AND organization_id = ?", agentID, org).Error; err != nil {
		return gitid.Identity{}, errors.New("unknown agent")
	}
	return s.git.For(&a), nil
}

func (s *Service) refusePush(w http.ResponseWriter, r *http.Request, sess *models.ChatSession, agentID string, meta map[string]any, err error) {
	meta["refused"] = err.Error()
	s.audit.Best(r.Context(), audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: agentID,
		Action: "git.push_refused", TargetType: "session", TargetID: sess.ID, Metadata: meta})
	logger.Warn("git push refused", "agent", agentID, "session", sess.ID, "refs", meta["refs"], "reason", err)
	writeReceivePackError(w, err.Error())
}

// sessionRepo checks the session and returns its project and forge client.
func (s *Service) sessionRepo(ctx context.Context, agentID, sessionID string) (*models.ChatSession, *models.Project, forgeClient, error) {
	var sess models.ChatSession
	if err := s.db.WithContext(ctx).First(&sess, "id = ? AND agent_id = ?", sessionID, agentID).Error; err != nil {
		return nil, nil, nil, errors.New("unknown session")
	}
	if sess.Status != models.SessionOpen {
		return nil, nil, nil, errors.New("session is closed")
	}
	if sess.ProjectID == nil || sess.Branch == "" {
		return nil, nil, nil, errors.New("session is not bound to a project")
	}
	proj, fg, err := s.projectForge(ctx, sess.OrganizationID, *sess.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	return &sess, proj, fg, nil
}

type pushCommand struct {
	Old, New, Ref string
}

// readPushCommands reads the pkt-line command list that starts a receive-pack request, up to the
// flush packet. It returns the raw command bytes and the start of the pack it already read, so the
// request can be replayed upstream.
func readPushCommands(r io.Reader) ([]byte, []byte, []pushCommand, error) {
	var raw bytes.Buffer
	br := bufio.NewReader(io.LimitReader(r, maxPushCommands))
	var cmds []pushCommand
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return raw.Bytes(), nil, cmds, fmt.Errorf("malformed push: %w", err)
		}
		raw.Write(hdr[:])
		n, err := strconv.ParseUint(string(hdr[:]), 16, 16)
		if err != nil {
			return raw.Bytes(), nil, cmds, errors.New("malformed push: bad pkt-line length")
		}
		if n == 0 { // flush: end of the command list
			break
		}
		if n < 4 {
			return raw.Bytes(), nil, cmds, errors.New("malformed push: unexpected special packet")
		}
		line := make([]byte, n-4)
		if _, err := io.ReadFull(br, line); err != nil {
			return raw.Bytes(), nil, cmds, fmt.Errorf("malformed push: %w", err)
		}
		raw.Write(line)
		text := strings.TrimSuffix(string(line), "\n")
		if i := strings.IndexByte(text, 0); i >= 0 {
			text = text[:i] // capabilities follow the first command
		}
		if strings.HasPrefix(text, "shallow ") {
			continue
		}
		if strings.HasPrefix(text, "push-cert") {
			return raw.Bytes(), nil, cmds, errors.New("signed pushes are not supported")
		}
		f := strings.Fields(text)
		if len(f) != 3 {
			return raw.Bytes(), nil, cmds, errors.New("malformed push command")
		}
		cmds = append(cmds, pushCommand{Old: f[0], New: f[1], Ref: f[2]})
	}
	// The pack data that follows was read into br's buffer; hand it back with the commands.
	buffered, _ := br.Peek(br.Buffered())
	return raw.Bytes(), bytes.Clone(buffered), cmds, nil
}

// isProbe reports whether a receive-pack request held only a flush packet and nothing after it.
func isProbe(packHead []byte, rest io.Reader) bool {
	if len(packHead) > 0 {
		return false
	}
	var one [1]byte
	n, _ := io.ReadFull(rest, one[:])
	return n == 0
}

// checkPush allows only creating or fast-forwarding the session's own branch.
func checkPush(cmds []pushCommand, branch string) error {
	if len(cmds) == 0 {
		return errors.New("empty push")
	}
	want := "refs/heads/" + branch
	for _, c := range cmds {
		if c.Ref != want {
			return fmt.Errorf("push to %s refused: this session may only push %s", c.Ref, want)
		}
		if c.New == zeroSHA {
			return fmt.Errorf("deleting %s is refused", c.Ref)
		}
	}
	return nil
}

// writeReceivePackError answers a refused push in a form git prints to the user ("remote: ...").
func writeReceivePackError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.WriteHeader(http.StatusOK)
	line := "\x03" + "akili: " + msg + "\n" // sideband 3: fatal error
	_, _ = fmt.Fprintf(w, "%04x%s0000", len(line)+4, line)
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

var gitClientTimeout = 30 * time.Minute
