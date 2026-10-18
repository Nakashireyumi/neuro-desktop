//go:build windows

package main

// NOT VERIFIED ON WINDOWS. This file is compiled by CI (cross-compile) but the
// shell path has not been run on a Windows machine. Python's shell=True uses
// COMSPEC /c; this does the same with exec.Command, which may quote the command
// line differently from Python. Check it before relying on shell_command here.

import (
	"context"
	"os"
	"os/exec"
)

// newShellCommand runs the command line through COMSPEC (cmd.exe) with /c.
func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	return exec.CommandContext(ctx, comspec, "/c", command)
}

// configureProcessGroup has nothing to set on Windows: the default cancel kills the
// shell process only.
func configureProcessGroup(cmd *exec.Cmd) {}

func exitCode(state *os.ProcessState) int { return state.ExitCode() }
