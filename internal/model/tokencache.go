package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Token persistence: the minted arb_auth_token is cached on disk between runs so
// an OTP-protected account doesn't have to re-authenticate every launch. The
// token lives ~1h server-side; a stale one is caught by the 401 re-mint path.

// tokenTTL is how long a cached token is trusted before we re-login. The API
// token lives ~1h (Max-Age 3600); the margin avoids using one about to expire.
const tokenTTL = 55 * time.Minute

type cachedToken struct {
	Token   string `json:"token"`
	SavedAt int64  `json:"saved_at"` // unix seconds
}

// tokenCacheFile returns a per-account cache path under the user's cache dir, or
// "" when it can't be determined (caching is then a no-op). Keyed by a hash of
// the username so the email is never written into the path.
func tokenCacheFile(username string) string {
	if username == "" {
		return ""
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(username))
	return filepath.Join(dir, "fftui", "token-"+hex.EncodeToString(sum[:])[:16]+".json")
}

// loadToken returns a cached token from path when it exists and is younger than
// tokenTTL; ok is false on any miss (missing, unreadable, malformed, expired).
func loadToken(path string, now time.Time) (token string, ok bool) {
	if path == "" {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c cachedToken
	if err := json.Unmarshal(raw, &c); err != nil || c.Token == "" {
		return "", false
	}
	if now.Sub(time.Unix(c.SavedAt, 0)) > tokenTTL {
		return "", false
	}
	return c.Token, true
}

// saveToken writes token to path (creating the dir, 0600). Best-effort: caching
// failures never block login.
func saveToken(path, token string, now time.Time) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if raw, err := json.Marshal(cachedToken{Token: token, SavedAt: now.Unix()}); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}

// clearToken removes a cached token, best-effort.
func clearToken(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
