//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package ytdlp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const processGroupGracePeriod = 2 * time.Second

// configureProcessGroup isolates yt-dlp and all helpers it starts. On context
// cancellation the group first receives SIGTERM and then SIGKILL, ensuring an
// ffmpeg child cannot survive its parent during container shutdown.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pid := cmd.Process.Pid
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}

		// cmd.Wait cannot reap and recycle the group leader while Cancel is
		// running, so escalation cannot accidentally target a reused process id.
		time.Sleep(processGroupGracePeriod)
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
}
