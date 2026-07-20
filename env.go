package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv reads KEY=VALUE lines from a .env file and sets any variable that
// isn't already present in the environment (a real env var always wins). A
// missing file is not an error. Values are taken literally — no variable
// expansion — so a password containing $ is safe. Supports an optional `export`
// prefix, # comments, and single- or double-quoted values.
//
// It looks in the working directory first, then next to the executable, so the
// app finds .env whether you `go run .` from the project or double-click the exe.
func loadDotEnv() {
	paths := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	for _, p := range paths {
		applyDotEnv(p)
	}
}

func applyDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))
		if key != "" {
			if _, set := os.LookupEnv(key); !set {
				os.Setenv(key, val)
			}
		}
	}
}

// unquote strips a single pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
