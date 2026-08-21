package gitworkspace

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

type StateLocker struct {
	Path    string
	Timeout time.Duration
}

func (l StateLocker) WithShared(ctx context.Context, fn func(context.Context) error) error {
	return l.with(ctx, syscall.LOCK_SH, fn)
}

func (l StateLocker) WithExclusive(ctx context.Context, fn func(context.Context) error) error {
	return l.with(ctx, syscall.LOCK_EX, fn)
}

func (l StateLocker) with(ctx context.Context, mode int, fn func(context.Context) error) error {
	file, err := os.OpenFile(l.Path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return WrapError("internal_error", "Failed to open Git Workspace state lock.", err)
	}
	defer file.Close()
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		err = syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return WrapError("internal_error", "Failed to acquire Git Workspace state lock.", err)
		}
		select {
		case <-ctx.Done():
			return WrapError("state_lock_timeout", "Canceled while waiting for the Git Workspace state lock.", ctx.Err())
		case <-deadline.C:
			return NewError("state_lock_timeout", "Timed out waiting for another Git Workspace command to finish.")
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn(ctx)
}
