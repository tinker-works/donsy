package colima

import (
	"context"
	"fmt"
	"strings"
)

// dockerBridgeNetwork is the block the profile's daemon is pinned to.
//
// It has to be pinned, and this is not tidiness. Colima sets no bip and no
// address pools, so dockerd uses its own defaults — docker0 at 172.17.0.1/16
// and user-defined bridges out of 172.17.0.0/12 — which sit *inside* the
// 172.16.0.0/12 the firewall below drops. Left alone, the egress rules would
// silently break container-to-container traffic. Pinning the daemon to a block
// the rules can name exempts Docker's own networking and nothing else.
const (
	dockerBridgeIP      = "172.19.0.1/24"
	dockerBridgeNetwork = "172.19.0.0/16"
)

// dockerDaemonConfig is written into the VM before the firewall is applied.
const dockerDaemonConfig = `{"bip":"` + dockerBridgeIP + `",` +
	`"default-address-pools":[{"base":"` + dockerBridgeNetwork + `","size":24}]}`

// privateRanges are the networks a container must not reach: the host's own
// LAN, link-local, and the carrier-grade block. This is the escape hatch a
// compromised or merely confused agent would otherwise have into the rest of
// the network.
var privateRanges = []string{
	"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
}

// colimaGateway is the VM's host gateway, and the resolver behind it. DNS has
// to survive the block or nothing in a container can resolve anything.
const colimaGateway = "192.168.5.2"

// prepareScript is the egress firewall, applied inside the VM.
//
// It lives in DOCKER-USER, the chain Docker consults for rules of its own that
// it will not overwrite, and which applies to every container in the profile at
// once. That is chosen over an nftables ruleset of our own for one practical
// reason: Colima's image installs iptables but not necessarily the nftables
// package, and "apt-get install" on first boot is a network dependency that
// would wedge every round of a project when it failed.
//
// It is reapplied on every profile start, and flushes first, because in-kernel
// rules do not survive a reboot and dockerd rebuilds its chains when it starts.
// That makes it idempotent by construction rather than by care.
//
// What it does not cover: DOCKER-USER hooks FORWARD, so traffic addressed to
// the VM itself is not filtered. A round holding the docker socket is already
// inside that boundary, which is why the profile is per project.
func prepareScript() string {
	var builder strings.Builder
	builder.WriteString("set -eu\n")
	builder.WriteString("install -d /etc/docker\n")
	builder.WriteString("cat >/etc/docker/daemon.json <<'GO_MERGE_DAEMON'\n")
	builder.WriteString(dockerDaemonConfig + "\n")
	builder.WriteString("GO_MERGE_DAEMON\n")
	// Restarting only on a change keeps a profile start from bouncing every
	// container the previous round left stopped-but-present.
	builder.WriteString("if ! cmp -s /etc/docker/daemon.json /etc/docker/daemon.json.applied; then\n")
	builder.WriteString("  cp /etc/docker/daemon.json /etc/docker/daemon.json.applied\n")
	builder.WriteString("  systemctl restart docker\n")
	builder.WriteString("fi\n")
	builder.WriteString("iptables -N DOCKER-USER 2>/dev/null || true\n")
	builder.WriteString("iptables -F DOCKER-USER\n")
	builder.WriteString(
		"iptables -A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN\n")
	builder.WriteString(
		"iptables -A DOCKER-USER -d " + dockerBridgeNetwork + " -j RETURN\n")
	for _, protocol := range []string{"udp", "tcp"} {
		builder.WriteString("iptables -A DOCKER-USER -d " + colimaGateway +
			" -p " + protocol + " --dport 53 -j RETURN\n")
	}
	for _, network := range privateRanges {
		builder.WriteString("iptables -A DOCKER-USER -d " + network + " -j DROP\n")
	}
	builder.WriteString("ip6tables -F DOCKER-USER 2>/dev/null || true\n")
	for _, network := range []string{"fc00::/7", "fe80::/10"} {
		builder.WriteString("ip6tables -A DOCKER-USER -d " + network + " -j DROP 2>/dev/null || true\n")
	}
	return builder.String()
}

// prepareProfile applies the daemon configuration and the firewall.
//
// A failure here fails the profile, loudly. The firewall is the boundary
// between an agent and the rest of the network, and running rounds without it
// is worse than running none — so this is deliberately not best-effort.
func (c *Client) prepareProfile(ctx context.Context, profile string) error {
	err := c.bounded(ctx, prepareTimeout,
		"colima", "ssh", "--profile", profile, "--", "sudo", "sh", "-c", prepareScript())
	if err != nil {
		return fmt.Errorf("prepare Colima profile %q: %w", profile, err)
	}
	return nil
}

// dockerSocketGID reads the group that owns the profile's docker socket.
//
// The number is assigned when the VM's image is built, so it is read rather
// than assumed. An unreadable socket is not an error: the group is only used to
// let a non-root agent reach the daemon, and a round that does not ask for the
// socket does not care.
func (c *Client) dockerSocketGID(ctx context.Context, profile string) string {
	output, err := c.boundedOutput(ctx, listTimeout,
		"colima", "ssh", "--profile", profile, "--", "stat", "-c", "%g", "/var/run/docker.sock")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
