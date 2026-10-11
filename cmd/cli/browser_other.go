//go:build !unix

package main

import "os/exec"

// detach leaves the browser where it starts: off unix there is no process
// group to move it out of.
func detach(cmd *exec.Cmd) {}
