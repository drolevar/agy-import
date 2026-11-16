//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package client

import "os/exec"

// CommandContext terminates the direct helper on these platforms. WaitDelay
// also bounds inherited-pipe waits. Descendant-tree termination is not claimed.
func configureProcess(cmd *exec.Cmd) {}
