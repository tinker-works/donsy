package slug

import "testing"

func TestParse(t *testing.T) {
	for _, value := range []string{"project-1", "a", "1-project"} {
		if _, err := Parse(value); err != nil {
			t.Errorf("Parse(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "Project", "project name", "-project", "project-", "project--one"} {
		if _, err := Parse(value); err == nil {
			t.Errorf("Parse(%q) succeeded", value)
		}
	}
}
