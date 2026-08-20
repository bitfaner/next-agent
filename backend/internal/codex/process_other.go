//go:build !windows

package codex

import "os/exec"

func configureChildProcess(_ *exec.Cmd) {}
