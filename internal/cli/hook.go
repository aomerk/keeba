package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/context"
)

// newHookCmd is the root for hook integrations. v1 ships one subcommand
// — `user-prompt-submit` — designed to run as a Claude Code
// UserPromptSubmit hook. The hook reads a JSON envelope on stdin
// ({prompt, cwd, ...}), runs keeba context against the cwd's symbol
// graph, and emits the resulting markdown back as the
// `additionalContext` field of a hookSpecificOutput JSON response.
//
// This is the invisible pre-grounding path: zero per-prompt action by
// the user, the agent sees the relevant file:line evidence in its
// system context before it picks any tool. Closes the gap A/B testing
// exposed where Claude needed an explicit "use keeba" prompt nudge to
// pick the right tool.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Claude Code hook handlers (keeba is invoked by the Claude Code agent loop, not the user).",
	}
	cmd.AddCommand(newUserPromptSubmitCmd())
	return cmd
}

// userPromptSubmitInput is the shape Claude Code sends on stdin for the
// UserPromptSubmit hook. We only need `prompt` and `cwd` — other fields
// (session_id, transcript_path) are ignored but accepted via the
// catch-all map so future-Claude-Code-version additions don't break us.
type userPromptSubmitInput struct {
	Prompt string `json:"prompt"`
	CWD    string `json:"cwd"`
}

// userPromptSubmitOutput is the JSON shape Claude Code reads back. The
// `additionalContext` string gets injected as a system reminder before
// the model sees the user's prompt — transparent to the user, visible
// to the model.
type userPromptSubmitOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// hookOutputCap is the safety margin under Claude Code's 10 KB hook
// output cap. Going over wastes the cap without adding signal.
const hookOutputCap = 8000

func newUserPromptSubmitCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:    "user-prompt-submit",
		Short:  "Read UserPromptSubmit hook JSON on stdin, emit keeba-context block as additionalContext.",
		Hidden: true, // not for direct user invocation — Claude Code calls it
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Always exit 0 — a non-zero exit blocks the user's prompt,
			// and a hook failure should never be more disruptive than no
			// hook at all. We swallow every error and emit the empty-
			// context response on the way out.
			return runUserPromptSubmit(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), verbose)
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "log decisions to stderr (hook output stays clean on stdout)")
	return cmd
}

// runUserPromptSubmit is the testable core. Reads + parses input,
// resolves the symbol-graph context, writes the JSON response.
// Always returns nil so the cobra wrapper exits 0.
func runUserPromptSubmit(stdin io.Reader, stdout, stderr io.Writer, verbose bool) error {
	body, err := io.ReadAll(stdin)
	if err != nil {
		emitEmpty(stdout)
		if verbose {
			_, _ = fmt.Fprintln(stderr, "keeba hook: read stdin:", err)
		}
		return nil
	}
	var in userPromptSubmitInput
	if err := json.Unmarshal(body, &in); err != nil {
		emitEmpty(stdout)
		if verbose {
			_, _ = fmt.Fprintln(stderr, "keeba hook: parse stdin:", err)
		}
		return nil
	}

	prompt := strings.TrimSpace(in.Prompt)
	cwd := strings.TrimSpace(in.CWD)
	if prompt == "" || cwd == "" {
		emitEmpty(stdout)
		return nil
	}

	rep, err := context.Build(cwd, prompt, context.Options{MaxBytes: hookOutputCap})
	if err != nil {
		// Most common failure: no .keeba/symbols.json. Silent — no
		// agent harm, no spam. The user already chose to install the
		// hook; if they haven't compiled, that's their next step.
		emitEmpty(stdout)
		if verbose {
			_, _ = fmt.Fprintln(stderr, "keeba hook: build:", err)
		}
		return nil
	}
	// Skip injection when the prompt has no code-shaped tokens AND no
	// quoted literals AND BM25 returned nothing. Empty payload means
	// keeba couldn't ground anything — better to inject zero context
	// than to spam the agent with "keeba: nothing relevant" noise.
	if len(rep.NameHits) == 0 && len(rep.BM25Hits) == 0 && len(rep.LiteralHits) == 0 {
		emitEmpty(stdout)
		if verbose {
			_, _ = fmt.Fprintln(stderr, "keeba hook: no hits, skipping")
		}
		return nil
	}

	md := context.RenderMarkdown(rep)
	if len(md) > hookOutputCap {
		md = md[:hookOutputCap] + "\n\n_…truncated to hookOutputCap_\n"
	}
	out := userPromptSubmitOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: md,
		},
	}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(out); err != nil {
		emitEmpty(stdout)
		if verbose {
			_, _ = fmt.Fprintln(stderr, "keeba hook: encode:", err)
		}
	}
	return nil
}

// emitEmpty writes the no-op hook response. Claude Code accepts this
// and proceeds with the user's original prompt — same as if the hook
// hadn't run at all.
func emitEmpty(stdout io.Writer) {
	out := userPromptSubmitOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: "",
		},
	}
	_ = json.NewEncoder(stdout).Encode(out)
}
