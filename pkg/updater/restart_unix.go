//go:build !windows

package updater

import (
	"syscall"
)

// execRestart replaces the current process image with the new binary on Unix systems.
func execRestart(executable string, args []string, env []string) error {
	return syscall.Exec(executable, args, env)
}
