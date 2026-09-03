//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package ytdlp

import (
	"os"
	"os/exec"
)

// configureProcessGroup is the portable fallback for platforms without Unix
// process groups. The production target is Alpine Linux and uses the Unix
// implementation above.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(os.Interrupt)
	}
}
