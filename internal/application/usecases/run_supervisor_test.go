package usecases

import (
	"context"
	"testing"
	"time"
)

func TestRunSupervisor_WaitShouldHoldUntilRoundCompletion(t *testing.T) {
	// Arrange
	supervisor := NewRunSupervisor()
	_, release := supervisor.Begin(context.Background(), "run-1")
	release()
	waited := make(chan error, 1)
	go func() {
		waited <- supervisor.Wait(context.Background(), "run-1")
	}()

	// Act: releasing cancellation is not enough; the use case still has to
	// persist the terminal run state before cleanup may continue.
	select {
	case <-waited:
		t.Fatal("wait returned before the round completed")
	case <-time.After(10 * time.Millisecond):
	}
	supervisor.Complete("run-1")

	// Assert
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not return after the round completed")
	}
}
