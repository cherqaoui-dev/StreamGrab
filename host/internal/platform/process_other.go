//go:build !windows

package platform

import (
	"errors"
	"os/exec"
	"runtime"
)

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
