package colima

import (
	"fmt"
	"os/exec"
	"strings"
)

// requiredTools are the two binaries this adapter drives. Colima creates and
// runs the per-project machine; the docker CLI talks to the daemon inside it.
var requiredTools = []string{"colima", "docker"}

// CheckTooling fails the launch when the sandbox runtime is not installed.
//
// It is checked at startup rather than left to the first round, because that
// round would fail with `exec: "colima": executable file not found` recorded
// against a subject — which reads as the agent being broken rather than the
// machine being unequipped, and would repeat for every subject on the host.
func CheckTooling() error {
	var missing []string
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"go-merge runs its agents in containers and needs %s on PATH: install with %q",
		joinTools(missing), "brew install colima docker",
	)
}

func joinTools(tools []string) string {
	if len(tools) < 2 {
		return strings.Join(tools, "")
	}
	return strings.Join(tools[:len(tools)-1], ", ") + " and " + tools[len(tools)-1]
}
