package gitworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("GIT_WORKSPACE_LOCK_HELPER") != "1" {
		return
	}
	locker := StateLocker{Path: os.Getenv("GIT_WORKSPACE_LOCK_PATH"), Timeout: time.Second}
	err := locker.WithExclusive(context.Background(), func(context.Context) error {
		if err := os.WriteFile(os.Getenv("GIT_WORKSPACE_LOCK_READY"), []byte("ready"), 0o600); err != nil {
			return err
		}
		time.Sleep(600 * time.Millisecond)
		return nil
	})
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestStateLockerSerializesProcesses(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")
	ready := filepath.Join(dir, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(), "GIT_WORKSPACE_LOCK_HELPER=1", "GIT_WORKSPACE_LOCK_PATH="+lockPath, "GIT_WORKSPACE_LOCK_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(2 * time.Second)
	for !pathIsFile(ready) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !pathIsFile(ready) {
		t.Fatal("helper did not acquire lock")
	}
	locker := StateLocker{Path: lockPath, Timeout: 100 * time.Millisecond}
	err := locker.WithExclusive(context.Background(), func(context.Context) error { return nil })
	if ErrorCode(err) != "state_lock_timeout" {
		t.Fatalf("error=%v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := locker.WithExclusive(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}
