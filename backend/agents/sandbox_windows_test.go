//go:build windows

package agents

import (
	"os/exec"
	"testing"
	"time"
)

func TestAttachJobObjectAndKillTree(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	applyProcAttrs(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start: %v", err)
	}
	p := &proc{cmd: cmd, done: make(chan struct{})}
	p.job = attachJobObject(cmd)
	if p.job == 0 {
		_ = cmd.Process.Kill()
		t.Skip("job object unavailable")
	}
	p.killTree()

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process was not killed by job close")
	}
	p.releaseJob()
}
