// Command gen-cli-reference renders docs/cli-reference.md from its template.
//
// The template is a plain text/template that reads the command help output from
// the environment. This exists so the rendering needs nothing but the Go
// toolchain: it previously ran gomplate in a container, which is built on
// text/template and was used for nothing but these substitutions, so it was a
// pinned dependency and a floating image tag earning no keep.
//
// Run from the module root, which is where the Taskfile runs it.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// The generator renders exactly one document, so its paths are constants rather
// than arguments: there is no second caller to parameterise for.
const (
	templatePath = "docs/cli-reference.md.gotmpl"
	outputPath   = "docs/cli-reference.md"
)

// the help outputs the template interpolates, every one of which must be present
// and non-empty for the rendered document to be complete.
var requiredVars = []string{
	"ROOT_USAGE",
	"BACKUP_USAGE",
	"RESTORE_USAGE",
	"STATUS_USAGE",
	"CLEANUP_USAGE",
	"COMPLETION_USAGE",
}

func main() {
	if err := render(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func render() error {
	// .Env mirrors how the template already addresses the environment, so the
	// template itself needed no change when gomplate went away.
	env := make(map[string]string)

	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}

	// An empty value means its producer printed nothing, which would silently
	// publish a document with a section missing. missingkey=error below cannot
	// see this, because the key is present.
	for _, name := range requiredVars {
		if strings.TrimSpace(env[name]) == "" {
			return fmt.Errorf("%s is empty: its help command produced no output", name)
		}
	}

	// missingkey=error so a renamed variable fails the build rather than
	// writing "<no value>" into a committed document.
	tmpl, err := template.New(filepath.Base(templatePath)).
		Option("missingkey=error").
		ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	// rendered fully in memory first: a template error must not leave a
	// truncated document behind for the dirty-tree check to trip over.
	var buf bytes.Buffer

	if err := tmpl.Execute(&buf, struct{ Env map[string]string }{Env: env}); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	// 0o600 rather than something more permissive: the output is tracked by
	// git, which records no mode beyond the executable bit, so the creating
	// process's choice never reaches anyone else.
	if err := os.WriteFile(outputPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
