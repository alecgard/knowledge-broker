package runtime

import (
	"os/exec"
	"syscall"
)

// detachProcess sets process attributes so the child process runs in its own
// session and survives after the parent exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
