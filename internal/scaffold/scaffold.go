// Package scaffold writes a fresh wiki repo from embedded templates.
//
// Triggered by `keeba init <name>`. Idempotent: refuses to overwrite an
// existing non-empty directory unless --force is given. Templates live under
// templates/ next to this file and are bundled into the binary via embed.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed all:templates
var fsys embed.FS

// Vars are the substitution variables exposed to every template.
type Vars struct {
	Name         string
	Purpose      string
	LastVerified string
	AITool       string // "claude-code" | "cursor" | "codex" | "none"
}

// Defaults returns Vars with today's date and sensible defaults.
func Defaults(name string) Vars {
	return Vars{
		Name:         name,
		Purpose:      fmt.Sprintf("Knowledge base for %s.", name),
		LastVerified: time.Now().UTC().Format("2006-01-02"),
		AITool:       "claude-code",
	}
}

// Scaffold writes the wiki tree at outDir using the given variables.
//
// outDir must not exist or must be empty (unless force is true). Files are
// written with 0o644 / dirs 0o755.
func Scaffold(outDir string, v Vars, force bool) error {
	if err := preflight(outDir, force); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	return fs.WalkDir(fsys, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "templates" {
			return nil
		}
		rel := strings.TrimPrefix(path, "templates/")
		// Drop the .tmpl suffix; the rendered file is the path without it.
		dest := filepath.Join(outDir, strings.TrimSuffix(rel, ".tmpl"))
		// Some template dirs are named with a leading underscore that 'embed'
		// strips; nothing to map for now.
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		body, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read embed %s: %w", path, err)
		}
		// Only files ending in .tmpl get template-rendered. The rest are copied
		// verbatim so they can contain literal `{{ ... }}` (e.g. workflows).
		if strings.HasSuffix(path, ".tmpl") {
			tpl, err := template.New(filepath.Base(path)).Option("missingkey=error").Parse(string(body))
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			var buf bytes.Buffer
			if err := tpl.Execute(&buf, v); err != nil {
				return fmt.Errorf("render %s: %w", path, err)
			}
			body = buf.Bytes()
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir parent of %s: %w", dest, err)
		}
		return os.WriteFile(dest, body, 0o644)
	})
}

func preflight(outDir string, force bool) error {
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", outDir)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 || force {
		return nil
	}
	return fmt.Errorf("%s is not empty (use --force to overwrite)", outDir)
}
