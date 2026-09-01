package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

type LifecycleLock struct {
	path string
	file *os.File
	once sync.Once
	err  error
}

func AcquireLifecycleLock(ctx context.Context, path string) (*LifecycleLock, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, runnerError("could not create lifecycle lock directory", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, runnerError("could not open lifecycle lock", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &LifecycleLock{path: path, file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, runnerError("could not acquire lifecycle lock", err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, runnerError("lifecycle lock wait aborted", ctx.Err())
		case <-timer.C:
		}
	}
}

func AcquireUpLock(ctx context.Context, plan manifest.Plan) (*LifecycleLock, error) {
	return AcquireLifecycleLock(ctx, lifecycleLockPath(plan.State.Run))
}

func (lock *LifecycleLock) Release() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		file := lock.file
		lock.file = nil
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
			lock.err = runnerError("could not release lifecycle lock", err)
		}
		if err := file.Close(); err != nil && lock.err == nil {
			lock.err = runnerError("could not close lifecycle lock", err)
		}
	})
	return lock.err
}

func (lock *LifecycleLock) assertPath(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if lock == nil || lock.file == nil || lock.path != path {
		return runnerErrorf("lifecycle lock does not own %s", path)
	}
	return nil
}

func lifecycleLockPath(runDirectory string) string {
	return filepath.Join(runDirectory, "up.lock")
}
