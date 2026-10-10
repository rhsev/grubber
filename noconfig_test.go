package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain lets a test run the real binary: with GRUBBER_TEST_MAIN=1 the test
// executable behaves as grubber itself, so config and environment handling
// are exercised end to end, HOME included.
func TestMain(m *testing.M) {
	if os.Getenv("GRUBBER_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type grubberRun struct {
	stdout, stderr string
	code           int
}

// runGrubber runs main() in a subprocess with HOME set to home and every
// inherited GRUBBER_* variable removed; env adds variables back.
func runGrubber(t *testing.T, home, dir string, env []string, args ...string) grubberRun {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GRUBBER_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "GRUBBER_TEST_MAIN=1", "HOME="+home)
	cmd.Env = append(cmd.Env, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run grubber: %v", err)
	}
	return grubberRun{out.String(), errOut.String(), code}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// noConfigFixture returns a notes dir, a HOME whose config sets every default
// that changes extract's output, and a HOME without any config.
func noConfigFixture(t *testing.T) (notes, home, bareHome string) {
	root := t.TempDir()
	notes = filepath.Join(root, "notes")
	writeFile(t, filepath.Join(notes, "binder_Trip.md"), "---\ntitle: Trip\nkeywords: a, b\n---\n\n"+
		"```yaml\ntype: ref\nid: 101\nbinder: Trip, Alt\n```\n\n"+
		"```yaml\ntype: meeting\nwhen: 2026-10-10\n```\n")
	writeFile(t, filepath.Join(notes, "plain.md"), "---\ntitle: Plain\nkeywords: c, d\n---\n\nNo blocks.\n")

	home = filepath.Join(root, "home")
	writeFile(t, filepath.Join(home, ".config/grubber/config.yaml"), `defaults:
  blocks_only: true
  filters: ["type=meeting"]
  array_fields: [binder, keywords]
  inherit: []
  extensions: [.typ]
sets:
  x:
    path: `+notes+`
`)
	bareHome = filepath.Join(root, "bare")
	if err := os.MkdirAll(bareHome, 0o755); err != nil {
		t.Fatal(err)
	}
	return notes, home, bareHome
}

func TestNoConfigIgnoresConfigAndEnvironment(t *testing.T) {
	notes, home, bareHome := noConfigFixture(t)
	env := []string{
		"GRUBBER_ARRAY_FIELDS=binder,title",
		"GRUBBER_EXTENSIONS=.typ",
		"GRUBBER_NOTES=" + bareHome,
	}

	calls := [][]string{
		{"extract", notes},
		{"extract", notes, "-b", "--no-fill", "--inherit=", "--extensions=.md", "-f", "type=ref"},
		{"extract", notes, "--format", "tsv", "-f", "title=Trip"},
		{notes, "--format", "jsonl"},
	}
	for _, args := range calls {
		name := strings.ReplaceAll(strings.Join(args, " "), notes, "NOTES")
		t.Run(name, func(t *testing.T) {
			want := runGrubber(t, bareHome, bareHome, nil, args...)
			got := runGrubber(t, home, bareHome, env, append(args, "--no-config")...)
			if want.code != 0 || got.code != 0 {
				t.Fatalf("exit codes %d/%d, stderr %q / %q", want.code, got.code, want.stderr, got.stderr)
			}
			if want.stdout == "" {
				t.Fatal("reference run produced no output")
			}
			if got.stdout != want.stdout {
				t.Errorf("--no-config output differs from a run without config\n got: %s\nwant: %s", got.stdout, want.stdout)
			}
			// Without the switch the same config and environment do change
			// the result, so the comparison above is not vacuous.
			if plain := runGrubber(t, home, bareHome, env, args...); plain.stdout == want.stdout {
				t.Errorf("config and environment had no effect; fixture does not test anything")
			}
		})
	}
}

func TestNoConfigWithSetIsUsageError(t *testing.T) {
	notes, home, _ := noConfigFixture(t)
	for _, args := range [][]string{
		{"extract", "--no-config", "--set", "x"},
		{"extract", notes, "--no-config", "-s", "x"},
	} {
		r := runGrubber(t, home, home, nil, args...)
		if r.code != 2 {
			t.Errorf("%v: exit %d, want 2", args, r.code)
		}
		if !strings.Contains(r.stderr, "--no-config and --set") {
			t.Errorf("%v: stderr %q does not name the conflict", args, r.stderr)
		}
		if r.stdout != "" {
			t.Errorf("%v: unexpected output %q", args, r.stdout)
		}
	}
}

// With --no-config neither GRUBBER_NOTES nor the cwd stands in for a missing
// directory.
func TestNoConfigRequiresDirectory(t *testing.T) {
	notes, home, _ := noConfigFixture(t)
	r := runGrubber(t, home, notes, []string{"GRUBBER_NOTES=" + notes}, "extract", "--no-config")
	if r.code != 2 {
		t.Errorf("exit %d, want 2 (stderr %q)", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "needs a directory") {
		t.Errorf("stderr %q does not say what is missing", r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("unexpected output %q", r.stdout)
	}
}

// A set named with --set outranks the environment for every setting, as its
// path outranks $GRUBBER_NOTES; the environment still applies without a set.
func TestSetOutranksEnvironment(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "notes")
	writeFile(t, filepath.Join(notes, "a.md"), "---\ntitle: A\nkeywords: x, y\n---\n")
	writeFile(t, filepath.Join(notes, "b.typ"), "/*\n---\ntitle: B\n---\n*/\n")
	home := filepath.Join(root, "home")
	writeFile(t, filepath.Join(home, ".config/grubber/config.yaml"), `sets:
  s:
    path: `+notes+`
    array_fields: [keywords]
    extensions: [.md]
`)
	env := []string{"GRUBBER_ARRAY_FIELDS=title", "GRUBBER_EXTENSIONS=.typ"}

	withSet := runGrubber(t, home, root, env, "extract", "--set", "s", "--format", "jsonl")
	if withSet.code != 0 {
		t.Fatalf("exit %d: %s", withSet.code, withSet.stderr)
	}
	if !strings.Contains(withSet.stdout, `"keywords":["x","y"]`) || !strings.Contains(withSet.stdout, `"title":"A"`) {
		t.Errorf("set's array_fields did not win over the environment:\n%s", withSet.stdout)
	}
	if strings.Contains(withSet.stdout, `"B"`) {
		t.Errorf("set's extensions did not win over the environment:\n%s", withSet.stdout)
	}

	noSet := runGrubber(t, home, root, env, "extract", notes, "--format", "jsonl")
	if noSet.code != 0 {
		t.Fatalf("exit %d: %s", noSet.code, noSet.stderr)
	}
	if strings.Contains(noSet.stdout, `"A"`) {
		t.Errorf("without a set, GRUBBER_EXTENSIONS should limit the scan to .typ:\n%s", noSet.stdout)
	}
}
