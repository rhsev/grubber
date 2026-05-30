package main

import "testing"

// fakeGetwd returns a fixed sentinel cwd, so tests can tell "fell back to cwd"
// apart from "" without depending on the real working directory.
func fakeGetwd(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func TestResolveNotesDir(t *testing.T) {
	const cwd = "/FAKE/CWD"

	t.Run("explicit cli dir wins over everything", func(t *testing.T) {
		got := resolveNotesDir("/cli", "/set", "/env", true, fakeGetwd(cwd))
		if got != "/cli" {
			t.Fatalf("got %q, want /cli", got)
		}
	})

	t.Run("set path used when no cli dir", func(t *testing.T) {
		// absolute set path round-trips through expandPath unchanged
		got := resolveNotesDir("", "/set/path", "/env", false, fakeGetwd(cwd))
		if got != "/set/path" {
			t.Fatalf("got %q, want /set/path", got)
		}
	})

	t.Run("env used when no cli or set path", func(t *testing.T) {
		got := resolveNotesDir("", "", "/env", false, fakeGetwd(cwd))
		if got != "/env" {
			t.Fatalf("got %q, want /env", got)
		}
	})

	t.Run("cwd fallback when nothing set and no ndjson sources", func(t *testing.T) {
		got := resolveNotesDir("", "", "", false, fakeGetwd(cwd))
		if got != cwd {
			t.Fatalf("got %q, want %q", got, cwd)
		}
	})

	// Regression: a pure --from-ndjson run with no dir/set/env must NOT fall
	// back to the cwd. Previously expandPath("") returned the cwd and turned a
	// source-only replay into a cwd scan.
	t.Run("no cwd fallback when ndjson sources present", func(t *testing.T) {
		got := resolveNotesDir("", "", "", true, fakeGetwd(cwd))
		if got != "" {
			t.Fatalf("got %q, want empty (source-only, no cwd scan)", got)
		}
	})

	// Regression: an empty set path must be ignored, not expanded (expandPath("")
	// would resolve to the cwd).
	t.Run("empty set path is not expanded to cwd", func(t *testing.T) {
		got := resolveNotesDir("", "", "", true, fakeGetwd(cwd))
		if got == cwd {
			t.Fatalf("empty set path leaked the cwd %q", cwd)
		}
	})
}
