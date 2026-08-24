//go:build windows

package codingtools

import (
	"context"
	"os/exec"
	"strconv"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	// Running a caller's command is the declared purpose of the shell_command
	// and run_tests tools. The layer is off unless M365_ENABLE_CODE_TOOLS turns
	// it on, and the manager confines the working directory and bounds the
	// runtime and the output.
	// #nosec G204
	return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
}

func configureProcess(cmd *exec.Cmd) {}

func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
