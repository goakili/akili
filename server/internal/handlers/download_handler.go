// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkaninda/okapi"
)

// agentBinaries are the only files /downloads serves; a request names one exactly.
var agentBinaries = map[string]bool{"akili-agent-linux-amd64": true, "akili-agent-linux-arm64": true}

var digests sync.Map // path+modtime → hex sha256

// AgentDownload serves an agent binary, or its checksum in sha256sum format ("<name>.sha256").
func (h *Handlers) AgentDownload(c *okapi.Context) error {
	file := c.Param("file")
	name, checksum := strings.CutSuffix(file, ".sha256")
	if !agentBinaries[name] {
		return c.AbortNotFound("no such download")
	}
	if h.Cfg.AgentDownloadsDir == "" {
		return c.AbortNotFound("agent downloads are not configured on this control plane (AKILI_AGENT_DOWNLOADS_DIR)")
	}
	path := filepath.Join(h.Cfg.AgentDownloadsDir, name)
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return c.AbortNotFound("this agent binary is not available on the control plane")
	}
	c.SetHeader("Cache-Control", "no-cache")
	if checksum {
		sum, err := fileDigest(path, fi.ModTime())
		if err != nil {
			return c.AbortInternalServerError("checksum failed", err)
		}
		return c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(sum+"  "+name+"\n"))
	}
	c.SetHeader("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(c.ResponseWriter(), c.Request(), path)
	return nil
}

func fileDigest(path string, mod time.Time) (string, error) {
	key := path + "@" + mod.String()
	if v, ok := digests.Load(key); ok {
		return v.(string), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	digests.Store(key, sum)
	return sum, nil
}
