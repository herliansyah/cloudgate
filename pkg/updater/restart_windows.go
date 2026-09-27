//go:build windows

package updater

import (
	"os"
	"os/exec"
)

// execRestart spawns the new binary and terminates the current process on Windows.
func execRestart(executable string, args []string, env []string) error {
	var cmdArgs []string
	if len(args) > 1 {
		cmdArgs = args[1:]
	}
	cmd := exec.Command(executable, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
