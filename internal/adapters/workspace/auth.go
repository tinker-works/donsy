package workspace

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	sshconfig "github.com/kevinburke/ssh_config"
)

// trustedRemote keeps host-side Git operations away from the checkout's mutable
// origin. The checkout is writable inside a sandbox, so its config cannot be an
// authority for where host credentials are sent.
func trustedRemote(repository *git.Repository, remoteURL string) *git.Remote {
	return git.NewRemote(repository.Storer, &config.RemoteConfig{
		Name:  "origin",
		URLs:  []string{remoteURL},
		Fetch: []config.RefSpec{config.RefSpec("+refs/heads/*:refs/remotes/origin/*")},
	})
}

func authForURL(remote string) (transport.AuthMethod, error) {
	if strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "ssh://") {
		host := sshHost(remote)
		for _, identity := range sshIdentityFiles(host) {
			if auth, err := gitssh.NewPublicKeysFromFile("git", identity, ""); err == nil {
				return auth, nil
			}
		}
		if os.Getenv("SSH_AUTH_SOCK") != "" {
			if auth, err := gitssh.NewSSHAgentAuth("git"); err == nil {
				return auth, nil
			}
		}
		return nil, fmt.Errorf("no SSH identities found for %s", host)
	}
	if strings.HasPrefix(remote, "file://") || filepath.IsAbs(remote) {
		return nil, nil
	}
	return nil, fmt.Errorf("project remote must use SSH")
}

func sshHost(remote string) string {
	if strings.HasPrefix(remote, "ssh://") {
		if parsed, err := url.Parse(remote); err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	remote = strings.TrimPrefix(remote, "ssh://")
	if at := strings.LastIndex(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	if colon := strings.IndexByte(remote, ':'); colon >= 0 {
		remote = remote[:colon]
	}
	return remote
}

func sshIdentityFiles(host string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := append(sshconfig.GetAll(host, "IdentityFile"),
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"))
	seen := map[string]struct{}{}
	identities := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || path == "none" {
			continue
		}
		if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(home, ".ssh", path)
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		identities = append(identities, path)
	}
	return identities
}
