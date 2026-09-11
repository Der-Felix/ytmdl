package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/compose"
	"ytdm/backend/cmd/ytmdlctl/internal/config"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/internal/update"
)

// channelChoice is the update channel a command works with and where it came
// from.
//
// Precedence, documented in the update guide:
//  1. --channel on the command line, for this invocation only;
//  2. the installation setting the administrator chose in the UI
//     (settings row "updates.channel", read-only from the database);
//  3. the stable channel.
//
// The setting is the single source of truth: ytmdlctl never writes it, and a
// --channel that differs from it is reported, so UI and CLI cannot silently
// assume different channels.
type channelChoice struct {
	Channel update.Channel
	Source  string
	Stored  string // the installation setting, when it could be read
}

// resolveChannel applies the precedence. readStored returns the stored
// setting; an empty string means none was ever stored.
func resolveChannel(flagValue string, readStored func() (string, error)) (channelChoice, error) {
	stored, storedErr := "", error(nil)
	if readStored != nil {
		stored, storedErr = readStored()
		stored = strings.TrimSpace(stored)
	} else {
		storedErr = fmt.Errorf("no installation to read")
	}

	if strings.TrimSpace(flagValue) != "" {
		ch, err := update.ParseChannel(flagValue)
		if err != nil {
			return channelChoice{}, err
		}
		choice := channelChoice{Channel: ch, Source: "--channel flag"}
		if storedErr == nil {
			choice.Stored = stored
		}
		return choice, nil
	}

	if storedErr != nil {
		return channelChoice{Channel: update.DefaultChannel, Source: "default (installation setting could not be read)"}, nil
	}
	if stored == "" {
		return channelChoice{Channel: update.DefaultChannel, Source: "default (no channel chosen)"}, nil
	}
	ch, err := update.ParseChannel(stored)
	if err != nil {
		return channelChoice{Channel: update.DefaultChannel, Source: fmt.Sprintf("default (stored value %q is not a channel)", stored), Stored: stored}, nil
	}
	return channelChoice{Channel: ch, Source: "installation setting", Stored: stored}, nil
}

// describe prints the channel and warns when --channel overrides a different
// installation setting.
func (c channelChoice) describe(w io.Writer) {
	label := string(c.Channel)
	if c.Channel == update.ChannelDevelopment {
		label += " (prereleases)"
	}
	fmt.Fprintf(w, "Update channel:            %s [%s]\n", label, c.Source)
	if c.Source == "--channel flag" && c.Stored != "" {
		if stored, err := update.ParseChannel(c.Stored); err == nil && stored != c.Channel {
			fmt.Fprintf(w, "Note:                      --channel %s overrides the installation setting %q for this run only; the UI keeps showing %q.\n", c.Channel, stored, stored)
		}
	}
}

// installationChannelReader returns a read-only reader of the stored channel
// of the installation in projectDir, or nil when the deployment cannot be
// resolved.
func installationChannelReader(ctx context.Context, deps CLIDependencies, projectDir, explicitFile, explicitEngine string) func() (string, error) {
	loadedCfg, _ := config.Load(projectDir)
	persistedFile, persistedEngine := "", ""
	if loadedCfg != nil {
		persistedFile, persistedEngine = loadedCfg.ComposeFile, loadedCfg.Engine
	}
	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    false,
	})
	if err != nil || composeRes == nil || composeRes.IsAmbiguous || composeRes.SelectedFile == "" {
		return nil
	}
	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projectDir,
		ComposeFile:     composeRes.SelectedFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      false,
	})
	if err != nil || eng == nil {
		return nil
	}
	return channelReaderFor(ctx, eng, projectDir, composeRes.SelectedFile)
}

// channelReaderFor reads the stored channel through an already resolved
// deployment.
func channelReaderFor(ctx context.Context, eng engine.Engine, projectDir, composeFile string) func() (string, error) {
	if eng == nil || composeFile == "" {
		return nil
	}
	return func() (string, error) {
		envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
		return discovery.QueryUpdateChannel(ctx, eng, projectDir, composeFile, envVars["POSTGRES_USER"], envVars["POSTGRES_DB"])
	}
}

// checkCLIMatchesTarget ties the CLI to the release it installs. A prerelease
// may change the update contract itself, so it must be installed with the
// ytmdlctl binary published in the same release. For a regular release an
// older CLI is only reported.
func checkCLIMatchesTarget(cliVersion, target string, warn io.Writer) error {
	targetV, err := update.ParseSemVer(target)
	if err != nil {
		return fmt.Errorf("target version %q is not valid semver", target)
	}
	cliV, cliErr := update.ParseSemVer(cliVersion)
	if cliErr != nil {
		fmt.Fprintf(warn, "warning: ytmdlctl %q is a development build; the release ships ytmdlctl %s for this target.\n", cliVersion, targetV.String())
		return nil
	}
	if targetV.PreRelease != "" && cliV.Compare(targetV) != 0 {
		return fmt.Errorf("prerelease %s must be installed with ytmdlctl %s from the same release (this is ytmdlctl %s); download it from the release assets and verify it against SHA256SUMS", targetV.String(), targetV.String(), cliV.String())
	}
	if cliV.Compare(targetV) < 0 {
		fmt.Fprintf(warn, "warning: ytmdlctl %s is older than the target %s; the release ships the matching ytmdlctl %s.\n", cliV.String(), targetV.String(), targetV.String())
	}
	return nil
}
