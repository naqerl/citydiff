//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the browser in its own process group. A browser that was not
// running yet is started by the opener as this process's child, and ctrl-c on
// the viewer would otherwise close it with the viewer.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
