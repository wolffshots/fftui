package model

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	now := time.Unix(1_000_000, 0)

	if _, ok := loadToken(path, now); ok {
		t.Fatal("expected miss for absent file")
	}

	saveToken(path, "tok_abc", now)
	if tok, ok := loadToken(path, now.Add(10*time.Minute)); !ok || tok != "tok_abc" {
		t.Fatalf("fresh load: tok=%q ok=%v", tok, ok)
	}

	if _, ok := loadToken(path, now.Add(tokenTTL+time.Minute)); ok {
		t.Error("expected miss for expired token")
	}

	clearToken(path)
	if _, ok := loadToken(path, now); ok {
		t.Error("expected miss after clear")
	}

	// Empty path (no cache dir) must be a safe no-op.
	saveToken("", "x", now)
	if _, ok := loadToken("", now); ok {
		t.Error("empty path should miss")
	}
	clearToken("")
}

func TestTokenCacheFilePerAccount(t *testing.T) {
	a, b := tokenCacheFile("alice@example.com"), tokenCacheFile("bob@example.com")
	if a == "" || b == "" {
		t.Skip("no user cache dir in this environment")
	}
	if a == b {
		t.Error("different accounts should map to different cache files")
	}
	if strings.Contains(a, "alice@example.com") {
		t.Error("cache path should not contain the raw username")
	}
	if tokenCacheFile("") != "" {
		t.Error("empty username should have no cache file")
	}
}
