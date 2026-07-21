package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# a comment\n" +
		"\n" +
		"FF_USERNAME=test@example.com\n" +
		"export FF_PASSWORD='p@ss#word$123'\n" + // $ and # must stay literal
		"FF_BASE_URL=\"https://srv.futureforex.co.za\"\n" +
		"FF_CLIENT_ID=9999\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// A real env var must take precedence over the file.
	t.Setenv("FF_CLIENT_ID", "1234")
	// Ensure the others are unset going in.
	os.Unsetenv("FF_USERNAME")
	os.Unsetenv("FF_PASSWORD")
	os.Unsetenv("FF_BASE_URL")

	applyDotEnv(path)

	cases := map[string]string{
		"FF_USERNAME":  "test@example.com",
		"FF_PASSWORD":  "p@ss#word$123", // literal $/#, quotes stripped, export dropped
		"FF_BASE_URL":  "https://srv.futureforex.co.za",
		"FF_CLIENT_ID": "1234", // env wins over the file's 9999
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestParseFeeTiers(t *testing.T) {
	// The exact format documented in .env.example.
	tiers, err := parseFeeTiers("100000:35,150000:33,200000:30,300000:28,400000:25")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tiers) != 5 || tiers[0].Min != 100_000 || tiers[0].Rate != 0.35 ||
		tiers[4].Min != 400_000 || tiers[4].Rate != 0.25 {
		t.Errorf("unexpected tiers: %+v", tiers)
	}
	for _, bad := range []string{"", "100000", "abc:35", "100000:x", "200000:30,100000:35"} {
		if _, err := parseFeeTiers(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{
		`"quoted"`:   "quoted",
		`'quoted'`:   "quoted",
		`bare`:       "bare",
		`"unmatched`: `"unmatched`,
		``:           ``,
	} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}
