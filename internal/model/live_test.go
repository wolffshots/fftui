package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestLiveLoginAndFetch exercises the real CSRF login + data fetch end to end.
// Opt-in: runs only when FF_USERNAME/PASSWORD are set in the env, so it
// never hits the network in a normal `go test ./...`.
func TestLiveLoginAndFetch(t *testing.T) {
	if os.Getenv("FF_PASSWORD") == "" {
		t.Skip("no creds in env")
	}
	src := NewLiveSource()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cs, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("live fetch: %v", err)
	}
	t.Logf("minted token len=%d, fetched %d cycles", len(src.Token), len(cs))
	if len(cs) == 0 {
		t.Fatal("no cycles")
	}
	last := cs[len(cs)-1]
	t.Logf("newest cycle: %s start=%s ZarIn=%.2f ZarOut=%.2f profit=%.2f ret=%.4f%%",
		last.Code, last.StartDate.Format("2006-01-02"), last.ZarIn, last.ZarOut,
		last.NetProfit, last.Return()*100)
}

// TestExpiredTokenOTPLatch: when the token expires mid-session on an
// OTP-protected account, nobody can answer the challenge (OTPFunc is
// disconnected once the alt-screen UI starts) — and every login POST makes the
// server text the user a fresh code. The first re-mint attempt must latch the
// failure so unattended retries (auto-refresh ticks, r) fail fast instead of
// texting a code per tick for the rest of the session.
func TestExpiredTokenOTPLatch(t *testing.T) {
	// Keep tokenCacheFile away from the real user cache on any OS.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var loginPosts int
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case csrfPath:
			fmt.Fprint(w, `{"csrf_token":"tok"}`)
		case loginPath:
			loginPosts++
			fmt.Fprint(w, `{"type":"error.otp_required","detail":"code sent","otp_channels":["whatsapp"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer auth.Close()
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // the token has expired
	}))
	defer data.Close()

	s := &LiveSource{
		Token:    "expired",
		BaseURL:  data.URL,
		AuthURL:  auth.URL,
		Client:   &http.Client{Timeout: 5 * time.Second},
		Username: "user@example.com",
		Password: "hunter2",
	}
	ctx := context.Background()
	if _, err := s.authGet(ctx, "/api/anything/"); err == nil {
		t.Fatal("expired token with an unanswerable OTP should error")
	}
	if loginPosts != 1 {
		t.Fatalf("first attempt made %d login POSTs, want 1", loginPosts)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.authGet(ctx, "/api/anything/"); err == nil {
			t.Fatal("latched OTP failure should keep failing")
		}
	}
	if loginPosts != 1 {
		t.Fatalf("retries re-POSTed the login %d times in total; each POST texts the user a code", loginPosts)
	}
}
