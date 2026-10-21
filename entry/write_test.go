package entry

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Sys().(*syscall.Stat_t).Ino
}

func write(t *testing.T, path string, data []byte) error {
	t.Helper()
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return WriteInPlace(path, data, Hash(cur))
}

// The note stays the same file: inode and hard links survive, because
// nothing replaces it. This is what keeps a fileregister bookmark.
func TestWriteInPlaceKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	os.WriteFile(path, []byte("A long old paragraph.\n"), 0o644)
	link := filepath.Join(dir, "link.md")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	before := inode(t, path)
	if err := write(t, path, []byte("Short.\n")); err != nil {
		t.Fatal(err)
	}
	if got := inode(t, path); got != before {
		t.Errorf("inode changed: %d → %d", before, got)
	}
	if data, _ := os.ReadFile(link); string(data) != "Short.\n" {
		t.Errorf("hard link reads %q; a shorter text must leave no tail", data)
	}
	if _, err := os.Lstat(JournalPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("journal left behind: %v", err)
	}
}

// On macOS the flags, the ACL and extended attributes stay with the file.
func TestWriteInPlaceKeepsMacAttributes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS attributes")
	}
	path := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(path, []byte("old\n"), 0o644)
	for _, c := range [][]string{
		{"/usr/bin/chflags", "hidden", path},
		{"/bin/chmod", "+a", "staff allow read", path},
		{"/usr/bin/xattr", "-w", "com.example.kept", "yes", path},
	} {
		if err := exec.Command(c[0], c[1:]...).Run(); err != nil {
			t.Skipf("%s: %v", c[0], err)
		}
	}
	if err := write(t, path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("/bin/ls", "-leO", path).Output()
	if !strings.Contains(string(out), "hidden") || !strings.Contains(string(out), "allow read") {
		t.Errorf("flag or ACL lost:\n%s", out)
	}
	if v, _ := exec.Command("/usr/bin/xattr", "-p", "com.example.kept", path).Output(); strings.TrimSpace(string(v)) != "yes" {
		t.Errorf("xattr lost: %q", v)
	}
}

// A journal from an unfinished write stops the next one; note and journal stay.
func TestWriteInPlaceRefusesLeftoverJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(path, []byte("old\n"), 0o644)
	os.WriteFile(JournalPath(path), []byte("half"), 0o600)
	err := write(t, path, []byte("new\n"))
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("err = %v, want a conflict about the journal", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "old\n" {
		t.Errorf("note changed: %q", data)
	}
	if data, _ := os.ReadFile(JournalPath(path)); string(data) != "half" {
		t.Error("the leftover journal must stay for the user to compare")
	}
}

// A note changed since it was read is not overwritten, and the journal of
// the refused write is gone.
func TestWriteInPlaceConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(path, []byte("old\n"), 0o644)
	if err := WriteInPlace(path, []byte("new\n"), "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if _, err := os.Lstat(JournalPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("journal left behind after a refused write: %v", err)
	}
}

// Through a symlink the target is written, and the link stays a link.
func TestWriteInPlaceThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	os.WriteFile(target, []byte("old\n"), 0o644)
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := write(t, link, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if data, _ := os.ReadFile(target); string(data) != "new\n" {
		t.Errorf("target reads %q", data)
	}
}
