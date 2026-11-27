//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll            = windows.NewLazySystemDLL("ntdll.dll")
	ntSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	ntResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

const processSuspendResume = 0x0800

// Tree is a child process together with everything it starts. The processes
// share a job object, which Windows closes, killing them all, if the host
// exits for any reason.
type Tree struct {
	cmd *exec.Cmd
	mu  sync.Mutex
	job windows.Handle
}

// StartTree starts cmd inside its own job object. Cancelling the command's
// context kills the whole job.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	job, err := newKillOnCloseJob()
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	tree := &Tree{cmd: cmd, job: job}
	// The process starts suspended so it can't create children before it
	// belongs to the job.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if cmd.Cancel != nil {
		cmd.Cancel = tree.Kill
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		tree.Close()
		return nil, err
	}
	if err := adopt(job, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		tree.Close()
		return nil, err
	}
	return tree, nil
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// adopt puts a suspended process into the job and then lets it run.
func adopt(job windows.Handle, pid int) error {
	access := uint32(windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE | processSuspendResume)
	handle, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		return fmt.Errorf("assign process to job: %w", err)
	}
	if status, _, _ := ntResumeProcess.Call(uintptr(handle)); status != 0 {
		return fmt.Errorf("start process %d: status %#x", pid, status)
	}
	return nil
}

// Kill stops every process in the job.
func (t *Tree) Kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(t.job, 1)
}

// Close releases the job after the command has been waited for. Any process
// still left in it is killed.
func (t *Tree) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

type Suspender struct{}

func NewSuspender() Suspender { return Suspender{} }

func (Suspender) Suspend(pid int) error { return setTree(pid, ntSuspendProcess) }
func (Suspender) Resume(pid int) error  { return setTree(pid, ntResumeProcess) }

// setTree applies proc to a process and all of its descendants. yt-dlp.exe runs
// as a launcher with a child, so FFmpeg is usually a grandchild.
func setTree(pid int, proc *windows.LazyProc) error {
	var first error
	for _, id := range append(descendants(pid), pid) {
		if err := setOne(id, proc); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func setOne(pid int, proc *windows.LazyProc) error {
	handle, err := windows.OpenProcess(processSuspendResume, false, uint32(pid))
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

func descendants(root int) []int {
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
	children := map[int][]int{}
	for {
		parent := int(entry.ParentProcessID)
		children[parent] = append(children[parent], int(entry.ProcessID))
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	var ids []int
	seen := map[int]bool{root: true}
	queue := []int{root}
	for len(queue) > 0 {
		for _, child := range children[queue[0]] {
			if !seen[child] {
				seen[child] = true
				ids = append(ids, child)
				queue = append(queue, child)
			}
		}
		queue = queue[1:]
	}
	return ids
}

func OpenFolder(path string) error {
	return exec.Command("explorer", filepath.Clean(path)).Start()
}

// ShowInFolder selects a file in Explorer. Explorer needs the quotes around the
// path only, which Go's usual argument quoting doesn't produce, so the command
// line is written directly. Windows paths can't contain quotes.
func ShowInFolder(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + filepath.Clean(path) + `"`}
	return cmd.Start()
}
