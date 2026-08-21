package colima

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// imageRepository marks an image as one of ours, which is what makes it safe to
// delete the ones no longer in use.
const imageRepository = "go-merge/agent"

// imageLabel is set on every image built here so pruning can find them without
// parsing tags, and buildLabel carries the recipe version so a superseded image
// is recognisable from its metadata alone.
const (
	imageLabel = "go-merge.image"
	buildLabel = "go-merge.build"
)

// imageTag keys the image by everything that changes what is inside it: the
// pinned Ubuntu/OpenCode recipe and — when a repository sets one — the content
// of its setup script.
//
// Keyed by the script's *content*, not by which repository asked for it: two
// repositories with byte-identical scripts, or no script at all, resolve to the
// same tag and share one image with no extra work needed to make that happen.
//
// Twelve hex characters, where the Lima golden image could only afford six: a
// tag is not also an instance name here, so there is no socket path to stay
// inside.
func imageTag(spec agent_runtime.SandboxSpec) string {
	tag := "oc" + strings.ReplaceAll(opencodeVersion, ".", "-") +
		"-b" + strconv.Itoa(buildVersion)
	if digest := setupScriptDigest(spec.SetupScript); digest != "" {
		tag += "-s" + digest
	}
	return imageRepository + ":" + tag
}

func setupScriptDigest(script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(script))
	return fmt.Sprintf("%x", sum)[:12]
}

// ensureImage builds the agent image inside the project's profile if it is not
// already there, and returns its tag.
//
// The build is lazy for the same reason the profile's start is: it costs
// minutes and a couple of gigabytes, and a host that only ever drafts epics
// never needs one. The caller holds the profile's lock, which is what keeps two
// concurrent rounds from building the same tag twice.
func (c *Client) ensureImage(
	ctx context.Context, profile string, spec agent_runtime.SandboxSpec,
) (string, error) {
	tag := imageTag(spec)
	if err := c.docker(ctx, profile, listTimeout, "image", "inspect", tag); err == nil {
		return tag, nil
	} else if !isNoSuchImage(err) {
		return "", fmt.Errorf("inspect agent image %q: %w", tag, err)
	}
	// Pruning happens before the build rather than after, so it can never meet
	// the image it is about to produce.
	if err := c.pruneSupersededImages(ctx, profile); err != nil {
		return "", err
	}
	if err := c.buildImage(ctx, profile, spec, tag); err != nil {
		return "", fmt.Errorf("build agent image %q: %w", tag, err)
	}
	return tag, nil
}

// buildImage writes the three files the build needs into a context directory
// and hands it to docker.
//
// A directory rather than a Dockerfile streamed over stdin: the init script and
// the repository's setup script have to reach the build anyway, and a directory
// left on disk is something a failing build can be inspected against.
func (c *Client) buildImage(
	ctx context.Context, profile string, spec agent_runtime.SandboxSpec, tag string,
) error {
	if c.buildDir == "" {
		return fmt.Errorf("no build directory is configured to assemble the image context in")
	}
	if err := os.MkdirAll(c.buildDir, 0o700); err != nil {
		return err
	}
	directory, err := os.MkdirTemp(c.buildDir, "context-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	files := map[string]string{
		"Dockerfile":    renderDockerfile(spec),
		initScriptName:  initScript(),
		setupScriptName: setupScriptContent(spec.SetupScript),
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return c.docker(ctx, profile, imageBuildTimeout, "build",
		"--tag", tag,
		"--label", imageLabel+"=1",
		"--label", buildLabel+"="+strconv.Itoa(buildVersion),
		// Plain progress because this output is only ever read from a log.
		"--progress", "plain",
		directory,
	)
}

// pruneSupersededImages deletes the agent images this profile can no longer
// use.
//
// An image is keyed by everything baked into it, so bumping the OpenCode pin or
// the recipe does not replace the old image — it adds another one beside it,
// and nothing will ever name the old one again. Images built by the current
// recipe are kept whatever their tag, because which setup scripts are in use is
// not knowable from the daemon: those are bounded by the recipe version rather
// than accumulating forever.
//
// Failing to delete is not worth failing a round over. A profile that is short
// of disk will say so on the build itself, with a better error than this can.
func (c *Client) pruneSupersededImages(ctx context.Context, profile string) error {
	output, err := c.dockerOutput(ctx, profile, listTimeout, "images",
		"--filter", "label="+imageLabel,
		"--filter", "label!="+buildLabel+"="+strconv.Itoa(buildVersion),
		"--format", "{{.ID}}",
	)
	if err != nil {
		return err
	}
	var ids []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return c.docker(ctx, profile, deleteTimeout,
		append([]string{"image", "rm", "--force"}, ids...)...)
}
