package colima

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The ledger is one small host-side file per container, written before the
// container is created. It is the analogue of the rendered Lima definition the
// previous adapter kept per instance, and it exists for one reason:
//
// Inspect runs for every sandbox record on every worker tick. If answering it
// required the profile to be up, the sweep's own job — stopping a project's VM
// once nothing is using it — would be undone by the next tick's inspection, and
// no profile would ever stay stopped.
//
// With the profile down there is no daemon to ask, and "the container is gone"
// and "the container is stopped" are not the same answer: reporting Stopped for
// a deleted container sends reclaim to docker rm, which would start the VM
// again. A file on the host distinguishes them without waking anything.
type record struct {
	Profile string `json:"profile"`
	// Image is the tag the container was created from. Ensure recreates the
	// container when it no longer matches, which is what makes a rebuilt image
	// reach an existing subject.
	Image string `json:"image"`
	// Spec fingerprints everything else baked in at creation — the bind mounts
	// above all. Docker cannot change those on an existing container, so a spec
	// that has moved has to become a new container rather than a reused one.
	Spec string `json:"spec"`
}

func (c *Client) recordPath(name string) (string, error) {
	if c.stateDir == "" {
		return "", fmt.Errorf("no state directory is configured to record sandboxes in")
	}
	// The name reaches the filesystem, so it has to be a bare file name. Docker
	// would reject a name with a slash in it too, but this is the check that
	// keeps a caller's mistake from escaping the state directory.
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("sandbox name %q is not a bare name", name)
	}
	return filepath.Join(c.stateDir, name+".json"), nil
}

func (c *Client) readRecord(name string) (record, bool, error) {
	path, err := c.recordPath(name)
	if err != nil {
		return record{}, false, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	var held record
	if err := json.Unmarshal(contents, &held); err != nil {
		// A record this code cannot read is worse than none: it would report a
		// container that exists as Absent and leak it. Say so instead.
		return record{}, false, fmt.Errorf("parse sandbox record %q: %w", name, err)
	}
	return held, true, nil
}

// writeRecord is called before the container is created, never after. The
// orders are not equivalent: a record with no container behind it is harmless —
// Inspect reports Absent and drops it — while a container no record names is
// one nothing can find again.
func (c *Client) writeRecord(name string, held record) error {
	path, err := c.recordPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := json.Marshal(held)
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o600)
}

// removeRecord forgets a container. A record that is already gone is not an
// error: reclaim is retried, and the second attempt must not fail over work the
// first one finished.
func (c *Client) removeRecord(name string) error {
	path, err := c.recordPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove sandbox record %q: %w", name, err)
	}
	return nil
}

// recordedNames lists every container the ledger still holds for one profile.
// It is what pruneOrphanContainers compares the daemon against.
func (c *Client) recordedNames(profile string) (map[string]struct{}, error) {
	names := map[string]struct{}{}
	if c.stateDir == "" {
		return names, nil
	}
	entries, err := os.ReadDir(c.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return names, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), ".json")
		if !found {
			continue
		}
		held, exists, err := c.readRecord(name)
		if err != nil {
			return nil, fmt.Errorf("read sandbox record %q: %w", name, err)
		}
		if !exists || held.Profile != profile {
			continue
		}
		names[name] = struct{}{}
	}
	return names, nil
}

// forgetProjectRecords drops the ledger for a whole profile, for a project
// being forgotten. The VM is going with it, so what the records name is gone.
func (c *Client) forgetProjectRecords(projectID uint) error {
	names, err := c.recordedNames(profileName(projectID))
	if err != nil {
		return err
	}
	var errs []error
	for name := range names {
		if err := c.removeRecord(name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
