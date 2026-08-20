package instancelock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// An flock belongs to the open file, not to the process, so two Acquires in one
// test process contend exactly the way two donsy launches do.

func TestAcquire_WhenTheWorkspaceIsFree_ShouldTakeItAndRecordThePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if got, want := string(contents), strconv.Itoa(os.Getpid())+"\n"; got != want {
		t.Fatalf("lock file holds %q, want %q", got, want)
	}
}

func TestAcquire_WhenAnotherInstanceHoldsTheWorkspace_ShouldReportItsPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = holder.Release() }()

	_, err = Acquire(path)

	var held HeldError
	if !errors.As(err, &held) {
		t.Fatalf("second acquire returned %v, want a HeldError", err)
	}
	if held.PID != os.Getpid() {
		t.Fatalf("holder pid is %d, want %d", held.PID, os.Getpid())
	}
}

func TestRelease_ShouldHandTheWorkspaceToTheNextInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := holder.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	next, err := Acquire(path)

	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	_ = next.Release()
}

func TestTerminate_ShouldWaitForTheHolderToLetGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// The holder reacts to the signal the way a running app does: not at once,
	// so the wait has to poll rather than assume the first try succeeds.
	signal := func(pid int, sig syscall.Signal) error {
		go func() {
			time.Sleep(2 * pollInterval)
			_ = holder.Release()
		}()
		return nil
	}

	lock, err := terminate(path, 4321, syscall.SIGTERM, 5*time.Second, signal)

	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	_ = lock.Release()
}

func TestTerminate_WhenTheHolderKeepsTheWorkspace_ShouldGiveUpAfterTheGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = holder.Release() }()
	signal := func(pid int, sig syscall.Signal) error { return nil }

	_, err = terminate(path, 4321, syscall.SIGTERM, pollInterval/2, signal)

	var held HeldError
	if !errors.As(err, &held) {
		t.Fatalf("terminate returned %v, want a HeldError", err)
	}
}

func TestTerminate_WhenTheHolderIsAlreadyGone_ShouldTakeTheWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	signal := func(pid int, sig syscall.Signal) error { return syscall.ESRCH }

	lock, err := terminate(path, 4321, syscall.SIGTERM, time.Second, signal)

	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	_ = lock.Release()
}

func TestTerminate_WhenTheSignalIsRefused_ShouldReportWhy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donsy.lock")
	signal := func(pid int, sig syscall.Signal) error { return syscall.EPERM }

	_, err := terminate(path, 4321, syscall.SIGTERM, time.Second, signal)

	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("terminate returned %v, want EPERM", err)
	}
}

// departedPID is a PID no process holds, so signalling it reports ESRCH — the
// holder having already gone. Signalling this process instead would kill the
// test run, which is what the real signal does to a real holder.
const departedPID = 0x7FFFFFF

func TestHeldError_ShouldNameTheHolderWhenItRecordedOne(t *testing.T) {
	// Arrange: a PID of 0 leaves nothing to signal, so the message must not
	// invite the user to act on one.

	// Act
	anonymous := HeldError{}.Error()
	named := HeldError{PID: 4321}.Error()

	// Assert
	if strings.Contains(anonymous, "pid") {
		t.Fatalf("expected no PID in %q", anonymous)
	}
	if !strings.Contains(named, "4321") {
		t.Fatalf("expected the holder's PID in %q", named)
	}
}

func TestQuit_ShouldTakeTheWorkspaceOnceTheHolderIsGone(t *testing.T) {
	// Arrange: ESRCH is the holder having already gone, which is the outcome
	// being asked for — the wait is what decides whether the lock came free.
	path := filepath.Join(t.TempDir(), "donsy.lock")

	// Act
	lock, err := Quit(path, departedPID, time.Second)

	// Assert
	if err != nil {
		t.Fatalf("expected the free workspace to be taken: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
}

func TestForce_ShouldTakeTheWorkspaceWithoutWaitingForCleanup(t *testing.T) {
	// Arrange: nothing the holder was running gets to clean up after itself; the
	// next launch reconciles what is left behind.
	path := filepath.Join(t.TempDir(), "donsy.lock")

	// Act
	lock, err := Force(path, departedPID, time.Second)

	// Assert
	if err != nil {
		t.Fatalf("expected the free workspace to be taken: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
}

func TestQuit_ShouldRefuseWhenThereIsNoHolderToSignal(t *testing.T) {
	// Arrange: a lock file with no PID leaves nothing to ask.
	path := filepath.Join(t.TempDir(), "donsy.lock")

	// Act
	_, err := Quit(path, 0, time.Second)

	// Assert
	var held HeldError
	if !errors.As(err, &held) {
		t.Fatalf("expected a HeldError, got %v", err)
	}
}

func TestRelease_ShouldTolerateALockThatWasNeverTaken(t *testing.T) {
	// Arrange: the app releases on shutdown whether or not it ever acquired.
	var lock *Lock

	// Act & Assert
	if err := lock.Release(); err != nil {
		t.Fatalf("expected releasing nothing to succeed: %v", err)
	}
	if err := (&Lock{}).Release(); err != nil {
		t.Fatalf("expected releasing an empty lock to succeed: %v", err)
	}
}

func TestAcquire_ShouldReportAWorkspaceItCannotOpen(t *testing.T) {
	// Arrange: a lock path inside a directory that does not exist.
	path := filepath.Join(t.TempDir(), "missing", "donsy.lock")

	// Act
	_, err := Acquire(path)

	// Assert
	if err == nil {
		t.Fatal("expected an unopenable lock path to be reported")
	}
	var held HeldError
	if errors.As(err, &held) {
		t.Fatalf("expected an I/O error rather than a held lock, got %v", err)
	}
}
