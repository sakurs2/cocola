//go:build linux

package opensandbox

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// Linux setsid forks when its caller is already a process-group leader. The
// --wait flag must keep the observable process alive and preserve the real
// command's exit status across that fork.
func TestSetsidWaitTracksForkedCommandLifecycle(t *testing.T) {
	command := exec.Command("setsid", "--wait", "sh", "-c", "sleep 0.15; exit 23")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	started := time.Now()
	err := command.Run()
	elapsed := time.Since(started)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("setsid --wait exit = %v, want child exit 23", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("setsid --wait returned after %s, before the child completed", elapsed)
	}
}
