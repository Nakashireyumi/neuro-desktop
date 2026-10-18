//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// newShellCommand runs the command line with /bin/sh -c, as Python's shell=True does.
func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// configureProcessGroup starts the command in its own process group, so a timeout
// kills the shell and everything it started, not only the shell.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// exitCode is the process's exit code, or minus the signal number when it was
// killed by a signal, as Python's returncode reports it.
func exitCode(state *os.ProcessState) int {
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return -int(status.Signal())
	}
	return state.ExitCode()
}
