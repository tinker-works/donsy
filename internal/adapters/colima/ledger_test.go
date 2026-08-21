package colima

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClient_Record_ShouldRoundTrip(t *testing.T) {
	// Arrange
	client := clientWith(t, newFakeRunner())
	written := record{Profile: "gm-7", Image: "go-merge/agent:a-oc1-18-18-b1", Spec: "abc123"}

	// Act
	if err := client.writeRecord("gm-7-issue-coding", written); err != nil {
		t.Fatal(err)
	}
	read, exists, err := client.readRecord("gm-7-issue-coding")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !exists || read != written {
		t.Fatalf("read back %#v, want %#v", read, written)
	}
}

// The name reaches the filesystem, so a caller's mistake must not escape the
// state directory.
func TestClient_Record_ShouldRefuseANameThatIsNotBare(t *testing.T) {
	// Arrange
	client := clientWith(t, newFakeRunner())

	// Act, Assert
	for _, name := range []string{"", "a/b", `a\b`, "../escape"} {
		if err := client.writeRecord(name, record{Profile: "gm-7"}); err == nil {
			t.Fatalf("expected %q to be refused", name)
		}
	}
}

func TestClient_Record_ShouldRefuseWithoutAStateDirectory(t *testing.T) {
	// Arrange
	client := newClient(newFakeRunner())

	// Act
	_, _, err := client.readRecord("gm-7-issue-coding")

	// Assert
	if err == nil {
		t.Fatal("expected the missing state directory to be reported")
	}
}

// A record this code cannot read is worse than none: it would report a
// container that exists as Absent and leak it.
func TestClient_ReadRecord_ShouldReportOneItCannotParse(t *testing.T) {
	// Arrange
	client := clientWith(t, newFakeRunner())
	if err := os.MkdirAll(client.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(client.stateDir, "gm-7-issue-coding.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := client.readRecord("gm-7-issue-coding")

	// Assert
	if err == nil {
		t.Fatal("expected the unreadable record to be reported")
	}
}

func TestClient_RemoveRecord_ShouldAcceptOneThatIsAlreadyGone(t *testing.T) {
	// Arrange: reclaim is retried, and the second attempt must not fail over
	// work the first one finished.
	client := clientWith(t, newFakeRunner())

	// Act, Assert
	if err := client.removeRecord("never-written"); err != nil {
		t.Fatal(err)
	}
}

func TestClient_RecordedNames_ShouldSeeOnlyOneProfilesOwn(t *testing.T) {
	// Arrange
	client := clientWith(t, newFakeRunner())
	for name, profile := range map[string]string{
		"gm-7-a": "gm-7", "gm-7-b": "gm-7", "gm-9-a": "gm-9",
	} {
		if err := client.writeRecord(name, record{Profile: profile}); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	names, err := client.recordedNames("gm-7")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("expected two records for gm-7, got %v", names)
	}
	if _, held := names["gm-9-a"]; held {
		t.Fatal("expected another project's records to be invisible")
	}
}

func TestClient_RecordedNames_ShouldAcceptAStateDirectoryThatIsNotThereYet(t *testing.T) {
	// Arrange: the first launch on a host has written nothing.
	client := clientWith(t, newFakeRunner())

	// Act
	names, err := client.recordedNames("gm-7")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("expected no records, got %v", names)
	}
}

func TestClient_PruneOrphanContainers_ShouldAbortOnAMalformedRecord(t *testing.T) {
	// Arrange: pruning an unparseable ledger would delete a container whose
	// ownership this process can no longer establish.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" ps --all",
		response{output: "gm-7-protected\n"})
	if err := os.MkdirAll(client.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client.stateDir, "gm-7-protected.json"),
		[]byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	err := client.pruneOrphanContainers(context.Background(), "gm-7")

	// Assert
	if err == nil {
		t.Fatal("expected the malformed record to abort pruning")
	}
	if runner.ran("rm", "--force", "--volumes") {
		t.Fatalf("expected no container deletion after a ledger error: %v", runner.lines())
	}
}

func TestRetireLima_ShouldRunOnceAndSurviveAHostWithoutLimactl(t *testing.T) {
	// Arrange: no limactl on PATH, and a directory the previous runtime left.
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	images := filepath.Join(root, "images")
	if err := os.MkdirAll(images, 0o700); err != nil {
		t.Fatal(err)
	}

	// Act
	RetireLima(context.Background(), root)

	// Assert
	if _, err := os.Stat(images); !os.IsNotExist(err) {
		t.Fatal("expected the previous runtime's images to be handed back")
	}
	marker := filepath.Join(root, "state", "lima-retired")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the marker so this runs once: %v", err)
	}

	// Act again: a second launch must not repeat the work.
	if err := os.MkdirAll(images, 0o700); err != nil {
		t.Fatal(err)
	}
	RetireLima(context.Background(), root)

	// Assert
	if _, err := os.Stat(images); err != nil {
		t.Fatal("expected the second launch to skip retirement entirely")
	}
}

func TestPrepareScript_ShouldExemptDockersOwnSubnetBeforeDroppingPrivateRanges(t *testing.T) {
	// Arrange: dockerd's stock bridges live inside 172.16.0.0/12, which the
	// ruleset drops — so without pinning the daemon and allowing that block,
	// container-to-container traffic would break silently.
	script := prepareScript()

	// Act
	allow := strings.Index(script, "-d "+dockerBridgeNetwork+" -j RETURN")
	drop := strings.Index(script, "-d 172.16.0.0/12 -j DROP")

	// Assert
	if allow < 0 || drop < 0 {
		t.Fatalf("expected both rules:\n%s", script)
	}
	if allow > drop {
		t.Fatalf("expected docker's own subnet exempted first:\n%s", script)
	}
	if !strings.Contains(script, dockerBridgeIP) {
		t.Fatalf("expected the daemon pinned to that block:\n%s", script)
	}
}

// In-kernel rules do not survive a reboot and dockerd rebuilds its chains when
// it starts, so this is reapplied on every profile start and has to be
// idempotent by construction rather than by care.
func TestPrepareScript_ShouldFlushBeforeItAppends(t *testing.T) {
	// Arrange, Act
	script := prepareScript()

	// Assert
	flush := strings.Index(script, "iptables -F DOCKER-USER")
	first := strings.Index(script, "iptables -A DOCKER-USER")
	if flush < 0 || first < 0 || flush > first {
		t.Fatalf("expected a flush before the first append:\n%s", script)
	}
}

func TestPrepareScript_ShouldLetDNSThrough(t *testing.T) {
	// Arrange, Act: without this nothing in a container resolves anything.
	script := prepareScript()

	// Assert
	for _, protocol := range []string{"udp", "tcp"} {
		rule := "-d " + colimaGateway + " -p " + protocol + " --dport 53 -j RETURN"
		if !strings.Contains(script, rule) {
			t.Fatalf("expected %q:\n%s", rule, script)
		}
	}
}
