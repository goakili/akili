// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps only support SHA-1 reliably
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"strings"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
)

// TOTP parameters (RFC 6238 defaults, which every authenticator app supports).
const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew accepts the previous and next code, for clock drift.
	totpSkew = 1
	// TOTPIssuer names Akili in authenticator apps.
	TOTPIssuer = "Akili"
	// RecoveryCodeCount is how many single-use recovery codes a user gets.
	RecoveryCodeCount = 10
	// MFAChallengeTTL is how long a password-verified login waits for its second factor.
	MFAChallengeTTL = 5 * time.Minute
	mfaTokenTries   = 5
	mfaUserFailures = 10
	mfaUserWindow   = 15 * time.Minute
)

// Second-factor errors.
var (
	ErrInvalidCode     = errors.New("invalid authentication code")
	ErrMFAChallenge    = errors.New("sign-in expired; enter your email and password again")
	ErrMFALocked       = errors.New("too many failed codes; try again later")
	ErrTOTPEnabled     = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotEnabled  = errors.New("two-factor authentication is not enabled")
	ErrTOTPNoSetup     = errors.New("start the two-factor setup first")
	errTOTPUnavailable = errors.New("two-factor authentication needs the encryption box")
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpCode is the RFC 4226 HOTP value of key at counter step.
func totpCode(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// matchTOTP returns the time step that code matches around now, comparing in constant time.
func matchTOTP(key []byte, code string, now time.Time) (int64, bool) {
	cur := now.Unix() / totpPeriod
	var found int64
	ok := 0
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, cur+d)), []byte(code)) == 1 {
			found, ok = cur+d, 1
		}
	}
	return found, ok == 1
}

func isTOTPCode(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeCode strips the spaces and dashes people type or paste with codes.
func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

func newRecoveryCodes() (plain, hashes []string) {
	for range RecoveryCodeCount {
		c := strings.ToLower(rand.Text()[:10])
		plain = append(plain, c[:5]+"-"+c[5:])
		hashes = append(hashes, crypto.HashToken(c))
	}
	return plain, hashes
}

// TOTPSetup is what an authenticator app needs to add the account.
type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
	// QRCode is a PNG data URL of URI.
	QRCode string `json:"qr_code"`
}

func otpauthURI(email, secret string) string {
	q := url.Values{"secret": {secret}, "issuer": {TOTPIssuer}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpPeriod)}}
	return "otpauth://totp/" + url.PathEscape(TOTPIssuer+":"+email) + "?" + q.Encode()
}

func qrDataURL(content string) (string, error) {
	code, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return "", err
	}
	if code, err = barcode.Scale(code, 240, 240); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, code); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (s *Service) totpKey(u *models.User) ([]byte, error) {
	if s.box == nil {
		return nil, errTOTPUnavailable
	}
	plain, err := s.box.Decrypt(u.TOTPSecret)
	if err != nil || plain == "" {
		return nil, ErrTOTPNoSetup
	}
	return b32.DecodeString(plain)
}

// BeginTOTP generates a new pending secret for u; it is used only once EnableTOTP confirms a code.
func (s *Service) BeginTOTP(ctx context.Context, u *models.User) (*TOTPSetup, error) {
	if u.TOTPEnabled {
		return nil, ErrTOTPEnabled
	}
	if s.box == nil {
		return nil, errTOTPUnavailable
	}
	key := make([]byte, 20)
	_, _ = rand.Read(key)
	secret := b32.EncodeToString(key)
	enc, err := s.box.Encrypt(secret)
	if err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND totp_enabled = ?", u.ID, false).Update("totp_secret", enc)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTOTPEnabled
	}
	u.TOTPSecret = enc
	uri := otpauthURI(u.Email, secret)
	img, err := qrDataURL(uri)
	if err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: secret, URI: uri, QRCode: img}, nil
}

// EnableTOTP turns on the pending secret after checking a code from it, and returns the recovery codes once.
func (s *Service) EnableTOTP(ctx context.Context, u *models.User, code string) ([]string, error) {
	if u.TOTPEnabled {
		return nil, ErrTOTPEnabled
	}
	if s.mfaBlocked(ctx, u) {
		return nil, ErrMFALocked
	}
	key, err := s.totpKey(u)
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(key, normalizeCode(code), time.Now())
	if !ok {
		RecordFailure(ctx, s.rdb, "mfa:"+u.ID, mfaUserWindow)
		return nil, ErrInvalidCode
	}
	plain, hashes := newRecoveryCodes()
	codes, _ := json.Marshal(hashes)
	res := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND totp_enabled = ? AND totp_secret = ?", u.ID, false, u.TOTPSecret).
		Updates(map[string]any{"totp_enabled": true, "totp_last_step": step, "recovery_codes": string(codes)})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTOTPNoSetup
	}
	u.TOTPEnabled, u.TOTPLastStep, u.RecoveryCodes = true, step, hashes
	return plain, nil
}

// DisableTOTP removes the second factor and its recovery codes.
func (s *Service) DisableTOTP(ctx context.Context, u *models.User) error {
	err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", u.ID).
		Updates(map[string]any{"totp_enabled": false, "totp_secret": "", "totp_last_step": 0, "recovery_codes": "[]"}).Error
	if err == nil {
		u.TOTPEnabled, u.TOTPSecret, u.TOTPLastStep, u.RecoveryCodes = false, "", 0, []string{}
	}
	return err
}

// RegenerateRecoveryCodes replaces every recovery code; the old ones stop working.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, u *models.User) ([]string, error) {
	if !u.TOTPEnabled {
		return nil, ErrTOTPNotEnabled
	}
	plain, hashes := newRecoveryCodes()
	codes, _ := json.Marshal(hashes)
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", u.ID).Update("recovery_codes", string(codes)).Error; err != nil {
		return nil, err
	}
	u.RecoveryCodes = hashes
	return plain, nil
}

func (s *Service) mfaBlocked(ctx context.Context, u *models.User) bool {
	return FailureBlocked(ctx, s.rdb, "mfa:"+u.ID, mfaUserFailures)
}

// VerifySecondFactor checks a TOTP or recovery code for u and consumes it. It returns the method used.
// Failures count per user across sign-ins and settings, so guessing is bounded whatever the entry point.
func (s *Service) VerifySecondFactor(ctx context.Context, u *models.User, code string) (string, error) {
	if !u.TOTPEnabled {
		return "", ErrTOTPNotEnabled
	}
	if s.mfaBlocked(ctx, u) {
		return "", ErrMFALocked
	}
	code = normalizeCode(code)
	var method string
	var err error
	if isTOTPCode(code) {
		method, err = "totp", s.useTOTP(ctx, u, code)
	} else {
		method, err = "recovery_code", s.useRecoveryCode(ctx, u, code)
	}
	if errors.Is(err, ErrInvalidCode) {
		RecordFailure(ctx, s.rdb, "mfa:"+u.ID, mfaUserWindow)
	}
	return method, err
}

func (s *Service) useTOTP(ctx context.Context, u *models.User, code string) error {
	key, err := s.totpKey(u)
	if err != nil {
		return err
	}
	step, ok := matchTOTP(key, code, time.Now())
	if !ok {
		return ErrInvalidCode
	}
	// The conditional update makes a code single-use even when two requests race.
	res := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND totp_last_step < ?", u.ID, step).Update("totp_last_step", step)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInvalidCode
	}
	u.TOTPLastStep = step
	return nil
}

func (s *Service) useRecoveryCode(ctx context.Context, u *models.User, code string) error {
	h := crypto.HashToken(code)
	rest := make([]string, 0, len(u.RecoveryCodes))
	found := 0
	for _, c := range u.RecoveryCodes {
		if subtle.ConstantTimeCompare([]byte(c), []byte(h)) == 1 {
			found = 1
			continue
		}
		rest = append(rest, c)
	}
	if found == 0 {
		return ErrInvalidCode
	}
	before, _ := json.Marshal(u.RecoveryCodes)
	after, _ := json.Marshal(rest)
	// Compare-and-swap on the whole list, so a code can't be spent twice by concurrent requests.
	res := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND recovery_codes = ?", u.ID, string(before)).Update("recovery_codes", string(after))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInvalidCode
	}
	u.RecoveryCodes = rest
	return nil
}

// StartMFA parks a password-verified user until the second factor arrives; the token is single-use.
func (s *Service) StartMFA(ctx context.Context, u *models.User) (string, time.Time, error) {
	token := crypto.NewToken("akm")
	if err := s.rdb.Set(ctx, "akili:mfa:"+crypto.HashToken(token), u.ID, MFAChallengeTTL).Err(); err != nil {
		return "", time.Time{}, err
	}
	return token, time.Now().UTC().Add(MFAChallengeTTL), nil
}

// CompleteMFA checks the second factor for a challenge and returns the user it belongs to.
func (s *Service) CompleteMFA(ctx context.Context, token, code string) (*models.User, string, error) {
	key := "akili:mfa:" + crypto.HashToken(token)
	uid, err := s.rdb.Get(ctx, key).Result()
	if err != nil || uid == "" {
		return nil, "", ErrMFAChallenge
	}
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", uid).Error; err != nil || !u.Active || (s.PasswordOwnerOnly && u.Role != models.RoleOwner) {
		s.rdb.Del(ctx, key)
		return nil, "", ErrMFAChallenge
	}
	method, err := s.VerifySecondFactor(ctx, &u, code)
	if err != nil {
		if n, _ := s.rdb.Incr(ctx, key+":tries").Result(); n == 1 {
			s.rdb.Expire(ctx, key+":tries", MFAChallengeTTL)
		} else if n >= mfaTokenTries {
			s.rdb.Del(ctx, key, key+":tries")
		}
		return &u, method, err
	}
	if n, _ := s.rdb.Del(ctx, key).Result(); n == 0 {
		return nil, "", ErrMFAChallenge
	}
	s.rdb.Del(ctx, key+":tries")
	return &u, method, nil
}
