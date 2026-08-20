package usecases

import (
	"strings"
	"testing"
)

func TestEvaluatePushGate(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		commits []RunCommit
		wantErr string
	}{
		{
			name: "ordinary work passes",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{"main.go", "main_test.go"}},
			},
		},
		{
			name:    "a round that changed nothing passes",
			base:    "base1234",
			commits: nil,
		},
		{
			name: "rewritten history is refused",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: false, Paths: []string{"main.go"}},
			},
			wantErr: "history was rewritten",
		},
		{
			name: "a workflow edit is refused",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{".github/workflows/ci.yml"}},
			},
			wantErr: "protected path",
		},
		{
			name: "CODEOWNERS is refused",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{".github/CODEOWNERS"}},
			},
			wantErr: "protected path",
		},
		{
			name: "a protected path in any commit fails the whole branch",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{"main.go"}},
				{Hash: "bbb", DescendsFromBase: true, Paths: []string{".github/workflows/ci.yml"}},
			},
			wantErr: "protected path",
		},
		{
			name: "case does not get past the gate",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{".GitHub/workflows/ci.yml"}},
			},
			wantErr: "protected path",
		},
		{
			name: "a nested .github directory is somebody's documentation",
			base: "base1234",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{"docs/.github/notes.md"}},
			},
		},
		{
			name: "no recorded base is a refusal, not a pass",
			base: "",
			commits: []RunCommit{
				{Hash: "aaa", DescendsFromBase: true, Paths: []string{"main.go"}},
			},
			wantErr: "no base commit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Act
			err := EvaluatePushGate(test.base, test.commits)

			// Assert
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the gate to pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected an error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}
