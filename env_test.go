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
