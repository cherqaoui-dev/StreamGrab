//go:build !windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// Tree is a child process together with everything it starts.
type Tree struct {
	cmd *exec.Cmd
}

// StartTree starts cmd in its own process group. Cancelling the command's
// context kills the whole group.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	tree := &Tree{cmd: cmd}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cmd.Cancel != nil {
		cmd.Cancel = tree.Kill
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return tree, nil
}

// Kill stops every process in the group.
func (t *Tree) Kill() error {
	err := syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// Close releases the tree after the command has been waited for.
func (t *Tree) Close() {}

type Suspender struct{}

func NewSuspender() Suspender { return Suspender{} }

func (Suspender) Suspend(int) error { return errors.New("suspend unavailable") }
func (Suspender) Resume(int) error  { return errors.New("resume unavailable") }

func OpenFolder(path string) error   { return open(path) }
func ShowInFolder(path string) error { return open(path) }

func open(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
