package colima

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Agent sandboxes used to be Lima VMs, one per subject and role, each a full
// copy of a multi-gigabyte golden image. A host that upgrades past that keeps
// all of it: stopped instances under the Lima home, and the golden images they
// were copied from, none of which anything will ever name again.
//
// Left alone it is tens of gigabytes of disk that looks like it is in use. This
// is exactly the failure the old adapter kept a list of retired image prefixes
// to avoid, so retiring the whole provider gets the same treatment.
const (
	retiredInstancePrefix = "gm-"
	retiredMarker         = "lima-retired"
	retireTimeout         = 5 * time.Minute
)

// RetireLima deletes what the previous sandbox runtime left on this host.
//
// Best effort throughout, and guarded by a marker so it runs once. Nothing here
// is needed for go-merge to work — it is disk being handed back — so a host
// without limactl, or one where a delete fails, gets a log line rather than a
// failed launch.
func RetireLima(ctx context.Context, root string) {
	marker := filepath.Join(root, "state", retiredMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := retireLima(ctx, root); err != nil {
		slog.Warn("retire the Lima sandbox runtime", "error", err)
	}
	// Written whatever happened. Retrying every launch would mean shelling out
	// to a provider this build does not use, forever, for a few gigabytes that
	// the user can also just delete.
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err == nil {
		_ = os.WriteFile(marker, nil, 0o600)
	}
}

func retireLima(ctx context.Context, root string) error {
	var errs []error
	if err := deleteLimaInstances(ctx); err != nil {
		errs = append(errs, err)
	}
	// The golden images and the rendered per-instance definitions. Both are
	// this adapter's predecessors' own directories, so removing them whole is
	// safe in a way removing anything under root at large would not be.
	for _, directory := range []string{"images", "vms", "sandboxes"} {
		if err := os.RemoveAll(filepath.Join(root, directory)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// deleteLimaInstances removes the agent VMs, and only those: names are matched
// against the prefix the old adapter minted, so a Lima instance the user
// created for something else is left alone. Colima's own instances live under a
// different Lima home and are not visible here at all.
func deleteLimaInstances(ctx context.Context) error {
	if _, err := exec.LookPath("limactl"); err != nil {
		return nil
	}
	listCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	output, err := command(listCtx, "limactl", "list", "--format", "json").Output()
	if err != nil {
		return fmt.Errorf("list Lima instances: %w", err)
	}
	var errs []error
	for line := range strings.SplitSeq(string(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var instance struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &instance); err != nil {
			continue
		}
		if !strings.HasPrefix(instance.Name, retiredInstancePrefix) {
			continue
		}
		deleteCtx, cancelDelete := context.WithTimeout(ctx, retireTimeout)
		err := command(deleteCtx, "limactl", "delete", "--force", instance.Name).Run()
		cancelDelete()
		if err != nil {
			errs = append(errs, fmt.Errorf("delete Lima instance %q: %w", instance.Name, err))
			continue
		}
		slog.Info("retired a Lima agent VM", "instance", instance.Name)
	}
	return errors.Join(errs...)
}
