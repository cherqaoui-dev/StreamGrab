//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll            = windows.NewLazySystemDLL("ntdll.dll")
	ntSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	ntResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

type Suspender struct{}

func NewSuspender() Suspender { return Suspender{} }

func (Suspender) Suspend(pid int) error { return setTree(pid, ntSuspendProcess) }
func (Suspender) Resume(pid int) error  { return setTree(pid, ntResumeProcess) }

func setTree(pid int, proc *windows.LazyProc) error {
	ids := append(childPIDs(pid), pid)
	var first error
	for _, id := range ids {
		if err := setOne(id, proc); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func setOne(pid int, proc *windows.LazyProc) error {
	handle, err := windows.OpenProcess(0x0800, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	status, _, callErr := proc.Call(uintptr(handle))
	if status != 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return callErr
		}
		return fmt.Errorf("process control failed for %d", pid)
	}
	return nil
}

func childPIDs(parent int) []int {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	var ids []int
	for {
		if int(entry.ParentProcessID) == parent {
			ids = append(ids, int(entry.ProcessID))
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return ids
}

func OpenFolder(path string) error {
	return exec.Command("explorer", path).Start()
}

func ShowInFolder(path string) error {
	return exec.Command("explorer", "/select,"+path).Start()
}
