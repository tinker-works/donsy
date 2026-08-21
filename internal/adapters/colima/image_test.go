package colima

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// Keyed by the script's content and not by which repository asked for it, so
// two repositories with identical scripts share one image with no extra work.
func TestImageTag_ShouldKeyBySetupScriptContent(t *testing.T) {
	// Arrange
	base := agent_runtime.SandboxSpec{}
	first := base
	first.SetupScript = "#!/bin/sh\napt-get install -y sqlite3\n"
	same := base
	same.SetupScript = first.SetupScript
	other := base
	other.SetupScript = "#!/bin/sh\napt-get install -y postgresql\n"

	// Act, Assert
	if imageTag(first) != imageTag(same) {
		t.Fatal("expected identical scripts to share one image")
	}
	if imageTag(first) == imageTag(other) {
		t.Fatal("expected different scripts to get different images")
	}
	if imageTag(first) == imageTag(base) {
		t.Fatal("expected a script to change the tag")
	}
}

func TestImageTag_ShouldUseTheFixedUbuntuRecipe(t *testing.T) {
	// Arrange: the base image is no longer part of the sandbox contract, so every
	// otherwise identical spec must address the same Ubuntu image.
	spec := agent_runtime.SandboxSpec{}

	// Act
	tag := imageTag(spec)

	// Assert
	if !strings.HasPrefix(tag, imageRepository+":oc") {
		t.Fatalf("expected the fixed recipe in %q", tag)
	}
	if !strings.Contains(tag, strings.ReplaceAll(opencodeVersion, ".", "-")) {
		t.Fatalf("expected the OpenCode pin in %q", tag)
	}
	if !strings.Contains(tag, "-b"+strconv.Itoa(buildVersion)) {
		t.Fatalf("expected the recipe version in %q", tag)
	}
}

func TestClient_EnsureImage_ShouldReuseAnImageThatIsAlreadyThere(t *testing.T) {
	// Arrange: the build is minutes and gigabytes, so it happens once.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	spec := testSpec(t)

	// Act
	tag, err := client.ensureImage(context.Background(), "gm-7", spec)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if tag != imageTag(spec) {
		t.Fatalf("unexpected tag %q", tag)
	}
	if runner.ran("build") {
		t.Fatalf("expected no build:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_EnsureImage_ShouldBuildAndLabelAMissingImage(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" image inspect", noSuchObject())
	spec := testSpec(t)
	spec.SetupScript = "#!/bin/sh\napt-get install -y sqlite3\n"

	// Act
	tag, err := client.ensureImage(context.Background(), "gm-7", spec)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("build", "--tag", tag, "--label", imageLabel+"=1") {
		t.Fatalf("expected a labelled build:\n%s", strings.Join(runner.lines(), "\n"))
	}
	// Pruning runs before the build, so it can never meet the image it is about
	// to produce.
	lines := runner.lines()
	prune, build := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "images --filter") {
			prune = index
		}
		if strings.Contains(line, "build --tag") {
			build = index
		}
	}
	if prune < 0 || build < 0 || prune > build {
		t.Fatalf("expected the prune before the build:\n%s", strings.Join(lines, "\n"))
	}
}

func TestClient_EnsureImage_ShouldPropagateANonNotFoundInspectError(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" image inspect",
		response{err: fmt.Errorf("permission denied")})

	// Act
	_, err := client.ensureImage(context.Background(), "gm-7", testSpec(t))

	// Assert
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the inspect error to be propagated, got %v", err)
	}
	if runner.ran("build") {
		t.Fatalf("expected no image build after an inspect failure: %v", runner.lines())
	}
}

func TestClient_EnsureImage_ShouldNotTreatAnUnrelatedNotFoundPhraseAsAbsence(t *testing.T) {
	// Arrange: only Docker's own error response may authorize a rebuild.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" image inspect",
		response{err: fmt.Errorf("permission denied: no such image: agent")})

	// Act
	_, err := client.ensureImage(context.Background(), "gm-7", testSpec(t))

	// Assert
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the inspect error to be propagated, got %v", err)
	}
	if runner.ran("build") {
		t.Fatalf("expected no image build after an inspect failure: %v", runner.lines())
	}
}

// An image is keyed by everything baked into it, so bumping the pin adds an
// image beside the old one rather than replacing it. Nothing will ever name the
// old one again.
func TestClient_PruneSupersededImages_ShouldRemoveOnlyOurOwnOldOnes(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" images",
		response{output: "sha-old\n\nsha-older\n"})

	// Act
	err := client.pruneSupersededImages(context.Background(), "gm-7")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("images", "--filter", "label="+imageLabel) {
		t.Fatalf("expected only labelled images to be considered:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
	if !runner.ran("image", "rm", "--force", "sha-old sha-older") {
		t.Fatalf("expected both superseded images removed:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_PruneSupersededImages_ShouldDoNothingWhenNoneAreSuperseded(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)

	// Act
	if err := client.pruneSupersededImages(context.Background(), "gm-7"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if runner.ran("image", "rm") {
		t.Fatalf("expected nothing removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_BuildImage_ShouldRefuseWithoutABuildDirectory(t *testing.T) {
	// Arrange: a context has to be written somewhere before docker can read it.
	client := newClient(newFakeRunner())

	// Act
	err := client.buildImage(context.Background(), "gm-7", testSpec(t), "tag")

	// Assert
	if err == nil {
		t.Fatal("expected the missing build directory to be reported")
	}
}
