// Package instancelock keeps one donsy per workspace.
//
// Two instances sharing a workspace share its store, its worker and its sandboxes:
// both tick every few seconds, both start rounds, and whichever exits last
// writes the state everyone else keeps. The guard is an flock on a file in the
// workspace root, held for the lifetime of the process. A lock file is chosen
// over a PID file because the kernel drops an flock when its holder dies, so a
// crash never leaves a lock nobody can clear. The holder's PID is written into
// the file anyway, so the second instance can name it and signal it.
package instancelock

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// pollInterval is how often a takeover re-tries the lock while it waits for the
// holder to go away.
const pollInterval = 200 * time.Millisecond

// Lock is a held workspace lock. Closing the file releases it, which the kernel
// also does on exit — so a lock that is never released explicitly is still gone
// once the process is.
type Lock struct{ file *os.File }

// HeldError reports that another process holds the lock. PID is 0 when the
// holder recorded none, which leaves nothing to signal.
type HeldError struct{ PID int }

func (e HeldError) Error() string {
	if e.PID == 0 {
		return "another donsy is already running"
	}
	return fmt.Sprintf("another donsy is already running (pid %d)", e.PID)
}

// Acquire takes the workspace lock, returning HeldError when another process
// already has it.
func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := readPID(file)
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, HeldError{PID: holder}
		}
		return nil, err
	}
	if err := writePID(file, os.Getpid()); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file}, nil
}

// Release gives the workspace back.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

// Quit asks the holder to shut down the way quitting the app does and waits for
// the lock to come free. Bubble Tea turns SIGTERM into a quit, so the holder
// unwinds its own shutdown — stopping the agent sandbox, closing the store — before
// the lock goes. Give it a grace period longer than that shutdown takes.
func Quit(path string, pid int, grace time.Duration) (*Lock, error) {
	return terminate(path, pid, syscall.SIGTERM, grace, syscall.Kill)
}

// Force kills the holder outright. Nothing it was running gets to clean up
// after itself; the next launch reconciles what is left behind.
func Force(path string, pid int, grace time.Duration) (*Lock, error) {
	return terminate(path, pid, syscall.SIGKILL, grace, syscall.Kill)
}

// terminate signals the holder and then polls for the lock until grace runs
// out, returning the holder's HeldError if it never lets go. The signal is a
// parameter so tests can drive the wait without a second process.
func terminate(
	path string,
	pid int,
	sig syscall.Signal,
	grace time.Duration,
	signal func(pid int, sig syscall.Signal) error,
) (*Lock, error) {
	if pid <= 0 {
		return nil, HeldError{}
	}
	// ESRCH is the holder having already gone, which is the outcome being asked
	// for — the wait below is what decides whether the lock actually came free.
	if err := signal(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return nil, err
	}
	deadline := time.Now().Add(grace)
	for {
		lock, err := Acquire(path)
		if err == nil {
			return lock, nil
		}
		var held HeldError
		if !errors.As(err, &held) || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(pollInterval)
	}
}

// readPID reads the PID the holder recorded, returning 0 for a file that has
// none yet: the holder locks before it writes, so the two are briefly apart.
func readPID(file *os.File) int {
	if _, err := file.Seek(0, 0); err != nil {
		return 0
	}
	contents := make([]byte, 32)
	n, _ := file.Read(contents)
	if n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents[:n])))
	if err != nil {
		return 0
	}
	return pid
}

func writePID(file *os.File, pid int) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	_, err := file.WriteString(strconv.Itoa(pid) + "\n")
	return err
}
