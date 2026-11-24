//go:build !windows

package main

import "os/exec"

// shellCommand runs s through sh -c.
func shellCommand(s string) *exec.Cmd {
	return exec.Command("sh", "-c", s)
}
