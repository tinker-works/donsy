package agent_runtime

import (
	"fmt"
	"path/filepath"
)

// GuestMountRoot is where every mount a round is given lands in the guest, and
// also the directory the agent is run from. Those have to be the same path:
// OpenCode treats anything outside its working directory as an external
// directory needing per-call approval, and a burst of those approvals raised in
// one step never resolves in a non-interactive run — the round then hangs until
// its runaway guard fires. Running from the root of what is mounted means no
// tool call is ever outside it.
//
// It lives here rather than in the runtime adapter because two adapters have to
// agree on it: one mounts to it, the other runs the agent from it.
const GuestMountRoot = "/work"

type SandboxMount struct {
	HostLocation  string
	GuestLocation string
	Writable      bool
}

func (m SandboxMount) Validate() error {
	if !filepath.IsAbs(m.HostLocation) || !filepath.IsAbs(m.GuestLocation) {
		return fmt.Errorf("sandbox host location and guest location must be absolute")
	}
	return nil
}
