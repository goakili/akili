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

package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
)

// Editor sign-in: the code alone is useless; the editor redeems it with its PKCE verifier for an API key.

// VSCodeExtensionID is the extension whose URI handler receives the code.
const VSCodeExtensionID = "goakili.akili"

const VSCodeKeyDays = 90

const vscodeCodeTTL = 2 * time.Minute

var vscodeSchemes = map[string]bool{"vscode": true, "vscode-insiders": true, "vscodium": true, "cursor": true, "windsurf": true}

var (
	pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	pkceVerifier  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	oauthState    = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)
	windowID      = regexp.MustCompile(`^[0-9]{1,10}$`)
)

var ErrVSCodeCode = errors.New("the sign-in code is invalid or expired; sign in again from the editor")

type vscodeGrant struct {
	UserID    string `json:"user_id"`
	Challenge string `json:"challenge"`
	Client    string `json:"client"`
}

// VSCodeRedirect fixes the URI shape here, so an approval can only reach the extension's URI handler, never a website.
func VSCodeRedirect(editor, window, code, state string) (string, error) {
	if !vscodeSchemes[editor] {
		return "", fmt.Errorf("unknown editor %q", editor)
	}
	if window != "" && !windowID.MatchString(window) {
		return "", errors.New("window must be a number")
	}
	if !oauthState.MatchString(state) {
		return "", errors.New("state must be 16-128 URL-safe characters")
	}
	q := url.Values{"code": {code}, "state": {state}}
	if window != "" {
		q.Set("windowId", window)
	}
	return (&url.URL{Scheme: editor, Host: VSCodeExtensionID, Path: "/callback", RawQuery: q.Encode()}).String(), nil
}

// ValidateClientName checks the editor's self-description, shown to the user and used in the API key's name.
func ValidateClientName(s string) error {
	if s == "" || len(s) > 80 || strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return errors.New("client must be 1-80 characters without control characters")
	}
	return nil
}

func (s *Service) IssueVSCodeCode(ctx context.Context, u *models.User, challenge, client string) (string, error) {
	if !pkceChallenge.MatchString(challenge) {
		return "", errors.New("challenge must be an S256 PKCE challenge")
	}
	if err := ValidateClientName(client); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(vscodeGrant{UserID: u.ID, Challenge: challenge, Client: client})
	code := crypto.NewToken("akc")
	if err := s.rdb.Set(ctx, "akili:vscode:"+crypto.HashToken(code), raw, vscodeCodeTTL).Err(); err != nil {
		return "", err
	}
	return code, nil
}

// RedeemVSCodeCode spends a code: a wrong verifier burns it too, so it can't be guessed at.
func (s *Service) RedeemVSCodeCode(ctx context.Context, code, verifier string) (*models.User, string, error) {
	if !strings.HasPrefix(code, "akc_") || !pkceVerifier.MatchString(verifier) {
		return nil, "", ErrVSCodeCode
	}
	raw, err := s.rdb.GetDel(ctx, "akili:vscode:"+crypto.HashToken(code)).Bytes()
	if err != nil {
		return nil, "", ErrVSCodeCode
	}
	var g vscodeGrant
	if json.Unmarshal(raw, &g) != nil || !VerifyPKCE(verifier, g.Challenge) {
		return nil, "", ErrVSCodeCode
	}
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", g.UserID).Error; err != nil || !u.Active {
		return nil, "", ErrVSCodeCode
	}
	return &u, g.Client, nil
}

// VerifyPKCE checks an S256 challenge (RFC 7636).
func VerifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}
