package main

import (
	"os/exec"
	"syscall"
)

// shellCommand runs s through cmd.exe. It passes the command line verbatim:
// exec.Command would escape inner double quotes as \", which cmd.exe does not
// understand, so a quoted argument such as "op://Private/My Item/password"
// would split at the space. /s strips only the outer quotes and keeps the rest.
func shellCommand(s string) *exec.Cmd {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + s + `"`}
	return cmd
}
