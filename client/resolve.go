package client

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveExecutable(configured string) (string, error) {
	if configured != "" {
		return resolveConfigured(configured)
	}
	if override := os.Getenv("AGY_IMPORT_BIN"); override != "" {
		return resolveConfigured(override)
	}
	name := "agy-import"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if consumer, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(consumer); err == nil {
			sibling := filepath.Join(filepath.Dir(real), name)
			if _, err := os.Stat(sibling); err == nil {
				return absoluteExecutable(sibling)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
	}
	return lookupAbsolutePATH(name)
}

func resolveConfigured(name string) (string, error) {
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsAny(name, `/\`) {
		absolute, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		return absoluteExecutable(absolute)
	}
	return lookupAbsolutePATH(name)
}

// Only absolute PATH entries participate, even when GODEBUG=execerrdot=0.
// Relative PATH entries are implicit searches rooted in the caller's cwd.
func lookupAbsolutePATH(name string) (string, error) {
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(directory) {
			continue
		}
		if path, err := absoluteExecutable(filepath.Join(directory, name)); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func absoluteExecutable(candidate string) (string, error) {
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		return "", exec.ErrDot
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("helper is not a regular executable file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}
