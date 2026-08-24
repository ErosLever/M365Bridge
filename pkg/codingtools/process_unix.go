//go:build !windows

package codingtools

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	// Running a caller's command is the declared purpose of the shell_command
	// and run_tests tools. The layer is off unless M365_ENABLE_CODE_TOOLS turns
	// it on, and the manager confines the working directory and bounds the
	// runtime and the output.
	// #nosec G204
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return errors.New("process not started")
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
