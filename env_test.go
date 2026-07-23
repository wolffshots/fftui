package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestUserConfigPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		got := userConfigPath()
		if !strings.HasSuffix(filepath.ToSlash(got), "fftui/config.env") {
			t.Errorf("userConfigPath() = %q, want …/fftui/config.env", got)
		}
		return
	}
	// XDG_CONFIG_HOME override is honoured verbatim.
	t.Setenv("XDG_CONFIG_HOME", "/xdg/here")
	if got, want := userConfigPath(), "/xdg/here/fftui/config.env"; got != want {
		t.Errorf("with XDG set: userConfigPath() = %q, want %q", got, want)
	}
	// Unset falls back to ~/.config.
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := userConfigPath(), filepath.Join(home, ".config", "fftui", "config.env"); got != want {
		t.Errorf("without XDG: userConfigPath() = %q, want %q", got, want)
	}
}

// TestApplyDotEnvPrecedence documents the first-wins chain for the user config
// file: a real env var beats it, and a cwd .env applied earlier beats it too.
func TestApplyDotEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	cwdEnv := filepath.Join(dir, ".env")
	userCfg := filepath.Join(dir, "config.env")
	if err := os.WriteFile(cwdEnv, []byte("FF_USERNAME=from-cwd\nFF_BASE_URL=from-cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte("FF_USERNAME=from-user\nFF_BASE_URL=from-user\nFF_AUTH_URL=from-user\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FF_USERNAME", "from-realenv") // real env must win over both files
	os.Unsetenv("FF_BASE_URL")
	os.Unsetenv("FF_AUTH_URL")

	// Same order loadDotEnv uses: cwd .env, then user config (first-wins).
	applyDotEnv(cwdEnv)
	applyDotEnv(userCfg)

	cases := map[string]string{
		"FF_USERNAME": "from-realenv", // real env beats every file
		"FF_BASE_URL": "from-cwd",     // cwd .env beats user config
		"FF_AUTH_URL": "from-user",    // only user config sets it
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestInitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.env")

	wrote, err := initConfig(path)
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if !wrote {
		t.Fatal("first initConfig should report wrote=true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("template not written: %v", err)
	}
	if string(got) != configTemplate {
		t.Error("written file does not match configTemplate")
	}

	// Second call must not overwrite and must report wrote=false.
	if err := os.WriteFile(path, []byte("SENTINEL=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrote, err = initConfig(path)
	if err != nil {
		t.Fatalf("second initConfig: %v", err)
	}
	if wrote {
		t.Error("second initConfig should report wrote=false")
	}
	if got, _ := os.ReadFile(path); string(got) != "SENTINEL=1\n" {
		t.Error("existing file was overwritten")
	}
}

func TestResolvePasswordCmd(t *testing.T) {
	echoCmd := "echo secret123"

	// Success: FF_PASSWORD is set from the command's trimmed stdout.
	t.Setenv("FF_PASSWORD", "")
	t.Setenv("FF_PASSWORD_CMD", echoCmd)
	if err := resolvePasswordCmd(); err != nil {
		t.Fatalf("resolvePasswordCmd: %v", err)
	}
	if got := os.Getenv("FF_PASSWORD"); got != "secret123" {
		t.Errorf("FF_PASSWORD = %q, want %q", got, "secret123")
	}

	// FF_PASSWORD already set takes precedence; the command must not run.
	t.Setenv("FF_PASSWORD", "explicit")
	t.Setenv("FF_PASSWORD_CMD", "echo should-not-run")
	if err := resolvePasswordCmd(); err != nil {
		t.Fatalf("resolvePasswordCmd: %v", err)
	}
	if got := os.Getenv("FF_PASSWORD"); got != "explicit" {
		t.Errorf("FF_PASSWORD = %q, want explicit (command should not override)", got)
	}

	// Failure surfaces an error ("exit 1" works for both sh -c and cmd /C).
	t.Setenv("FF_PASSWORD", "")
	t.Setenv("FF_PASSWORD_CMD", "exit 1")
	if err := resolvePasswordCmd(); err == nil {
		t.Error("expected error from failing FF_PASSWORD_CMD")
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
