package epic

import "testing"

func TestAggregateTransitionsRejectForeignStatuses(t *testing.T) {
	tests := []struct {
		name       string
		transition func() error
	}{
		{
			name: "epic cannot be approved",
			transition: func() error {
				value, err := NewEpic("Migration")
				if err != nil {
					return err
				}
				return value.Transition(StatusApproved)
			},
		},
		{
			name: "issue cannot be merged",
			transition: func() error {
				value, err := NewIssue("Write tests")
				if err != nil {
					return err
				}
				return value.Transition(StatusMerged)
			},
		},
		{
			name: "pull request cannot be done",
			transition: func() error {
				value, err := NewPullRequest("Review changes")
				if err != nil {
					return err
				}
				return value.Transition(StatusDone)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.transition(); err == nil {
				t.Fatal("Transition() accepted a status belonging to another aggregate")
			}
		})
	}
}

func TestAggregateValidationRejectsForeignStatuses(t *testing.T) {
	epicValue, err := NewEpic("Migration")
	if err != nil {
		t.Fatalf("NewEpic() error = %v", err)
	}
	epicValue.Status = StatusApproved
	if err := epicValue.Validate(); err == nil {
		t.Fatal("Epic.Validate() accepted approved status")
	}

	issueValue, err := NewIssue("Write tests")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	issueValue.Status = StatusMerged
	if err := issueValue.Validate(); err == nil {
		t.Fatal("Issue.Validate() accepted merged status")
	}

	requestValue, err := NewPullRequest("Review changes")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	requestValue.Status = StatusDone
	if err := requestValue.Validate(); err == nil {
		t.Fatal("PullRequest.Validate() accepted done status")
	}
}
