package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/ingest"
)

// runGitHubIngest is the github branch of `keeba ingest`.
func runGitHubIngest(cmd *cobra.Command, repo, since string, limit int, dryRun bool) error {
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}
	if repo == "" {
		repo, err = detectGitHubRepo()
		if err != nil {
			return err
		}
	}
	res, err := ingest.GitHub(cfg.WikiRoot, repo, since, limit, dryRun)
	if err != nil {
		return err
	}
	if len(res.Imported) == 0 && len(res.Skipped) == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"no durable signal in %s since %s (%d PRs scanned, all noise)\n",
			repo, since, len(res.Noise))
		return nil
	}
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"%s %d action(s); skipped %d already-imported; %d noise.\n",
		verb, len(res.Imported), len(res.Skipped), len(res.Noise))
	for _, s := range res.Imported {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  · %s\n", s)
	}
	return nil
}

// detectGitHubRepo parses the cwd's git remote and extracts owner/name.
// Supports both git@ and https:// origins. Falls back to an error so the
// user gets a clear "pass --repo" message.
func detectGitHubRepo() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output() //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("can't detect github repo (no `origin` remote?). Pass --repo OWNER/NAME")
	}
	url := strings.TrimSpace(string(out))
	// git@github.com:OWNER/NAME(.git)?
	if strings.HasPrefix(url, "git@github.com:") {
		body := strings.TrimPrefix(url, "git@github.com:")
		body = strings.TrimSuffix(body, ".git")
		return body, nil
	}
	// ssh://git@github.com/OWNER/NAME(.git)?
	if strings.HasPrefix(url, "ssh://git@github.com/") {
		body := strings.TrimPrefix(url, "ssh://git@github.com/")
		body = strings.TrimSuffix(body, ".git")
		return body, nil
	}
	// https://github.com/OWNER/NAME(.git)?
	for _, pre := range []string{"https://github.com/", "http://github.com/"} {
		if strings.HasPrefix(url, pre) {
			body := strings.TrimPrefix(url, pre)
			body = strings.TrimSuffix(body, ".git")
			return body, nil
		}
	}
	return "", fmt.Errorf("origin %q is not a recognized GitHub URL; pass --repo OWNER/NAME", url)
}
