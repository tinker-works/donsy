package main

import (
	"bytes"
	"testing"
)

func TestPrintDescription(t *testing.T) {
	var output bytes.Buffer

	if err := printDescription(&output); err != nil {
		t.Fatalf("printDescription() error = %v", err)
	}

	const want = "donsy is the go-merge daemon and host\n"
	if output.String() != want {
		t.Fatalf("printDescription() = %q, want %q", output.String(), want)
	}
}
