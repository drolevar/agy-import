package client

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func copyTestExecutable(t *testing.T, directory, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	original, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	path := filepath.Join(directory, name)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfiguredExecutableWins(t *testing.T) {
	first := copyTestExecutable(t, t.TempDir(), "first")
	second := copyTestExecutable(t, t.TempDir(), "second")
	t.Setenv("AGY_IMPORT_BIN", second)
	got, err := resolveExecutable(first)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(first)
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestInvalidConfiguredExecutableDoesNotFallThrough(t *testing.T) {
	second := copyTestExecutable(t, t.TempDir(), "second")
	t.Setenv("AGY_IMPORT_BIN", second)
	if _, err := resolveExecutable(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("invalid override silently fell through")
	}
}

func TestEnvironmentOverride(t *testing.T) {
	path := copyTestExecutable(t, t.TempDir(), "environment-helper")
	t.Setenv("AGY_IMPORT_BIN", path)
	got, err := resolveExecutable("")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(path)
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	t.Setenv("AGY_IMPORT_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := resolveExecutable(""); err == nil {
		t.Fatal("invalid environment override silently fell through")
	}
}

func TestPATHDiscoveryUsesAbsoluteEntries(t *testing.T) {
	directory := t.TempDir()
	path := copyTestExecutable(t, directory, "agy-import")
	t.Setenv("PATH", directory)
	t.Setenv("AGY_IMPORT_BIN", "")
	got, err := resolveExecutable("")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(path)
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	got, err = resolveExecutable(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("configured bare name did not use PATH")
	}
}

func TestSiblingDiscoveryPrecedesPATH(t *testing.T) {
	directory := t.TempDir()
	consumer := copyTestExecutable(t, directory, "fake-consumer")
	sibling := copyTestExecutable(t, directory, "agy-import")
	pathDirectory := t.TempDir()
	_ = copyTestExecutable(t, pathDirectory, "agy-import")
	t.Setenv("PATH", pathDirectory)
	t.Setenv("AGY_IMPORT_BIN", "")
	t.Setenv("AGY_IMPORT_CLIENT_DISCOVERY", "1")
	// Where available, invoke through a link elsewhere to ensure the sibling
	// belongs to the resolved consumer executable, not the link's directory.
	link := filepath.Join(t.TempDir(), filepath.Base(consumer))
	if err := os.Symlink(consumer, link); err == nil {
		consumer = link
	}
	got, err := exec.Command(consumer).Output()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(sibling)
	if string(got) != want {
		t.Fatalf("got %q; want sibling %q", got, want)
	}
}

func TestImplicitCurrentDirectoryIsRejected(t *testing.T) {
	t.Setenv("PATH", "."+string(os.PathListSeparator)+"")
	t.Setenv("GODEBUG", "execerrdot=0")
	_, err := lookupAbsolutePATH("agy-import")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected no absolute PATH candidate: %v", err)
	}
}

func TestExplicitRelativePathResolvesBeforeLaunch(t *testing.T) {
	directory := t.TempDir()
	path := copyTestExecutable(t, directory, "relative-helper")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The checkout and temporary directory may be on different Windows
	// volumes. Resolve from the temporary directory so the explicit relative
	// path stays on one volume without writing into the module directory.
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	relative := "." + string(filepath.Separator) + filepath.Base(path)
	got, err := resolveExecutable(relative)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(path)
	if !filepath.IsAbs(got) || got != want {
		t.Fatalf("got %q; want absolute %q", got, want)
	}
}

func TestDirectoryIsNotExecutable(t *testing.T) {
	if _, err := resolveExecutable(t.TempDir()); err == nil {
		t.Fatal("directory accepted as executable")
	}
}
