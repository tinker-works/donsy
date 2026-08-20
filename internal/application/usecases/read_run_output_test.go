package usecases

import (
	"fmt"
	"testing"
)

type fakeRunOutput struct {
	pages map[int64][]string
	next  map[int64]int64
	err   error
	asked []int64

	// sizes and sizeErr drive the activity sampling, which reads a length
	// without reading the log.
	sizes   map[string]int64
	sizeErr map[string]error

	discarded  []string
	discardErr error
}

func (o *fakeRunOutput) Size(runID string) (int64, error) {
	if err, ok := o.sizeErr[runID]; ok {
		return 0, err
	}
	return o.sizes[runID], nil
}

func (o *fakeRunOutput) Discard(runID string) error {
	o.discarded = append(o.discarded, runID)
	return o.discardErr
}

func (o *fakeRunOutput) Tail(_ string, from int64) ([]string, int64, error) {
	o.asked = append(o.asked, from)
	if o.err != nil {
		return nil, from, o.err
	}
	next, ok := o.next[from]
	if !ok {
		next = from
	}
	return o.pages[from], next, nil
}

func TestReadRunOutputUseCase_ShouldDecodeTheTranscript(t *testing.T) {
	// Arrange
	output := &fakeRunOutput{
		pages: map[int64][]string{0: {"hello"}},
		next:  map[int64]int64{0: 42},
	}
	useCase := &ReadRunOutputUseCase{output: output, builder: fakeCommandBuilder{}}

	// Act
	page, err := useCase.Handle(ReadRunOutputQuery{RunID: "run-1"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Text != "hello" {
		t.Fatalf("unexpected entries: %+v", page.Entries)
	}
	if page.Next != 42 {
		t.Fatalf("expected the resume offset, got %d", page.Next)
	}
}

func TestReadRunOutputUseCase_ShouldResumeFromTheGivenOffset(t *testing.T) {
	// Arrange: polling for live output reads only what is new.
	output := &fakeRunOutput{
		pages: map[int64][]string{42: {"more"}},
		next:  map[int64]int64{42: 60},
	}
	useCase := &ReadRunOutputUseCase{output: output, builder: fakeCommandBuilder{}}

	// Act
	page, err := useCase.Handle(ReadRunOutputQuery{RunID: "run-1", From: 42})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(output.asked) != 1 || output.asked[0] != 42 {
		t.Fatalf("expected a read from 42, got %v", output.asked)
	}
	if page.Next != 60 {
		t.Fatalf("expected offset 60, got %d", page.Next)
	}
}

func TestReadRunOutputUseCase_ShouldTreatANegativeOffsetAsTheStart(t *testing.T) {
	// Arrange
	output := &fakeRunOutput{pages: map[int64][]string{0: {"start"}}}
	useCase := &ReadRunOutputUseCase{output: output, builder: fakeCommandBuilder{}}

	// Act
	if _, err := useCase.Handle(ReadRunOutputQuery{RunID: "run-1", From: -5}); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(output.asked) != 1 || output.asked[0] != 0 {
		t.Fatalf("expected a read from 0, got %v", output.asked)
	}
}

func TestReadRunOutputUseCase_ShouldRequireARunID(t *testing.T) {
	// Arrange
	useCase := &ReadRunOutputUseCase{output: &fakeRunOutput{}, builder: fakeCommandBuilder{}}

	// Act
	_, err := useCase.Handle(ReadRunOutputQuery{RunID: "  "})

	// Assert
	if err == nil {
		t.Fatal("expected a missing run ID to be rejected")
	}
}

func TestReadRunOutputUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	output := &fakeRunOutput{err: fmt.Errorf("disk gone")}
	useCase := &ReadRunOutputUseCase{output: output, builder: fakeCommandBuilder{}}

	// Act
	_, err := useCase.Handle(ReadRunOutputQuery{RunID: "run-1"})

	// Assert
	if err == nil {
		t.Fatal("expected the read failure to surface")
	}
}
