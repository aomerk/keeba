package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/ingest"
)

// runGitHubIngest is the github branch of `keeba ingest`.
func runGitHubIngest(cmd *cobra.Command, repoFlag, since string, limit int, dryRun bool) error {
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}

	repo, source, err := resolveGitHubRepo(cmd, cfg.Ingest.GitHub.Repo, repoFlag)
	if err != nil {
		return err
	}
	// Persist the answer if the user just typed it in interactively.
	if source == "prompt" {
		if err := cfg.SaveGitHubRepo(repo); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: couldn't save repo to keeba.config.yaml: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"saved github.repo = %s to keeba.config.yaml\n", repo)
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

// resolveGitHubRepo finds the right OWNER/NAME, in priority order:
//
//  1. --github-repo flag (CLI override, never persisted).
//  2. ingest.github.repo from keeba.config.yaml (persisted choice).
//  3. Interactive prompt; the answer is persisted by the caller.
//
// The "source" return is one of "flag" / "config" / "prompt" so the caller
// knows whether to save the answer.
func resolveGitHubRepo(cmd *cobra.Command, fromConfig, fromFlag string) (string, string, error) {
	if fromFlag != "" {
		if !validRepoFormat(fromFlag) {
			return "", "", fmt.Errorf("--github-repo %q is not in OWNER/NAME format", fromFlag)
		}
		return fromFlag, "flag", nil
	}
	if fromConfig != "" {
		return fromConfig, "config", nil
	}
	// Interactive prompt. Suggest the cwd's origin remote if it parses as
	// GitHub, but never auto-pick it.
	suggestion := suggestGitHubRepoFromOrigin()
	prompt := "GitHub repo to ingest (OWNER/NAME): "
	if suggestion != "" {
		prompt = fmt.Sprintf("GitHub repo to ingest (OWNER/NAME) [%s]: ", suggestion)
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), prompt)
	in := bufio.NewReader(os.Stdin)
	line, err := in.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("read repo from stdin: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" && suggestion != "" {
		line = suggestion
	}
	if !validRepoFormat(line) {
		return "", "", fmt.Errorf("repo %q is not in OWNER/NAME format", line)
	}
	return line, "prompt", nil
}

var reOwnerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\/[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validRepoFormat(s string) bool {
	return reOwnerName.MatchString(s)
}

// suggestGitHubRepoFromOrigin returns OWNER/NAME parsed from the cwd's
// git origin remote, or "" if that's not a recognized GitHub URL. Never
// fatal — purely a UX hint for the prompt default.
func suggestGitHubRepoFromOrigin() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output() //nolint:gosec
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		return strings.TrimSuffix(strings.TrimPrefix(url, "git@github.com:"), ".git")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		return strings.TrimSuffix(strings.TrimPrefix(url, "ssh://git@github.com/"), ".git")
	}
	for _, pre := range []string{"https://github.com/", "http://github.com/"} {
		if strings.HasPrefix(url, pre) {
			return strings.TrimSuffix(strings.TrimPrefix(url, pre), ".git")
		}
	}
	return ""
}
