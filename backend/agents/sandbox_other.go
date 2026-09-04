//go:build !windows

package agents

import (
	"os/exec"
	"syscall"
)

// На unix процессы запускаются в отдельной группе (Setpgid): kill -pgid
// убивает runner и всех запущенных им детей разом.

type jobObject uintptr

func attachJobObject(*exec.Cmd) jobObject { return 0 }

func applyProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (p *proc) killTree() {
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	_ = p.cmd.Process.Kill()
}

func (p *proc) releaseJob() {}
