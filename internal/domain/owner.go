package domain

import (
	"fmt"
	"strings"
)

// SplitRepositoryRef splits a repository reference into its owner and name. A
// reference comes in two shapes: "owner/name", which is how the discovered pool
// and a project's linked set record one, and a git remote, which is how a
// project names the repository tracking it — "git@host:owner/name.git" and
// "https://host/owner/name" both name the same repository as "owner/name".
//
// A reference that names no owner — a bare "api", which a linked set is allowed
// to hold — reports false, as does anything deeper than owner/name, since that
// is not a repository this app can address.
func SplitRepositoryRef(ref string) (owner, name string, ok bool) {
	ref = strings.TrimSuffix(strings.TrimSpace(ref), ".git")
	// A remote carries a host in front of the owner, in one of two shapes:
	// "scheme://host/owner/name" and the scp-style "git@host:owner/name".
	if _, path, found := strings.Cut(ref, "://"); found {
		if _, afterHost, found := strings.Cut(path, "/"); found {
			ref = afterHost
		} else {
			ref = path
		}
	} else if _, path, found := strings.Cut(ref, ":"); found {
		ref = path
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner, name = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

// Owner is the account a repository belongs to: the "acme" in "acme/api".
func (r Repository) Owner() string {
	if r.Organisation != "" {
		return r.Organisation
	}
	owner, _, _ := SplitRepositoryRef(r.FullName)
	return owner
}

// SameOwner reports whether a project tracked under owner may work on ref.
// Owners are compared case-insensitively, the way GitHub treats them.
//
// Two references pass without a comparison: one carrying no owner of its own
// belongs to the project by construction, and a project whose remote names no
// owner has nothing to compare against — guessing there would hide repositories
// on the strength of a remote the app failed to read.
func SameOwner(owner, ref string) bool {
	refOwner, _, ok := SplitRepositoryRef(ref)
	if !ok || owner == "" {
		return true
	}
	return strings.EqualFold(refOwner, owner)
}

// EnsureSameOwner refuses a repository scope that reaches outside owner. It
// names every offender: a caller hands over the whole set at once — the settings
// screen replaces a project's linked list wholesale — so a refusal that named
// only the first would leave the rest to be discovered one save at a time.
func EnsureSameOwner(owner string, refs []string) error {
	foreign := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !SameOwner(owner, ref) {
			foreign = append(foreign, ref)
		}
	}
	if len(foreign) == 0 {
		return nil
	}
	return fmt.Errorf("%s not owned by %s: go-merge does not work across owners",
		strings.Join(foreign, ", "), owner)
}
