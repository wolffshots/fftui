package model

import (
	"context"
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
