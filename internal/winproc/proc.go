package winproc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Child is a claude.exe sharing baton's console, in its own process group.
type Child struct {
	Cmd  *exec.Cmd
	PID  int
	job  windows.Handle
	done chan struct{}
	code int
	err  error
}

// Start launches exe on the inherited console. CREATE_NEW_PROCESS_GROUP lets
// baton send CTRL_BREAK to claude (and its tool processes) without hitting
// itself. While claude's TUI runs, the console is in raw mode, so the user's
// Ctrl+C still reaches claude as a keystroke.
func Start(exe string, args, env []string, dir string) (*Child, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Child{Cmd: cmd, PID: cmd.Process.Pid, done: make(chan struct{})}
	// os/exec holds the process handle until Wait returns, so the pid cannot be
	// reused before this OpenProcess.
	c.job = newKillJob(c.PID)
	go func() {
		err := cmd.Wait()
		var ee *exec.ExitError
		switch {
		case err == nil:
			c.code = 0
		case errors.As(err, &ee):
			c.code = ee.ExitCode()
		default:
			c.code, c.err = -1, err
		}
		close(c.done)
	}()
	return c, nil
}

// Done is closed when the process has exited.
func (c *Child) Done() <-chan struct{} { return c.done }

func (c *Child) ExitCode() int { return c.code }

func (c *Child) Exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// CtrlBreak asks claude to shut down gracefully: it flushes the transcript,
// runs SessionEnd hooks, restores the terminal and exits 0.
func (c *Child) CtrlBreak() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(c.PID))
}

// WaitExit waits up to d; it reports whether the process exited.
func (c *Child) WaitExit(d time.Duration) bool {
	select {
	case <-c.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Kill terminates claude and everything it started (last resort: it skips
// claude's shutdown, leaves stale session files and the console dirty).
func (c *Child) Kill() {
	if c.job != 0 {
		_ = windows.TerminateJobObject(c.job, 1)
	}
	if !c.Exited() && c.Cmd.Process != nil {
		_ = c.Cmd.Process.Kill()
	}
}

// Close releases the job handle. The job has no KILL_ON_JOB_CLOSE, so if baton
// itself crashes the user's claude keeps running.
func (c *Child) Close() {
	if c.job != 0 {
		windows.CloseHandle(c.job)
		c.job = 0
	}
}

func newKillJob(pid int) windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		windows.CloseHandle(job)
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0
	}
	defer windows.CloseHandle(h)
	if windows.AssignProcessToJobObject(job, h) != nil {
		windows.CloseHandle(job)
		return 0
	}
	return job
}

// RunInNewConsole runs exe in a new console window and waits for it. Used when
// baton itself was started somewhere that is not a console (mintty without
// ConPTY, an IDE output pane), where claude's TUI cannot run.
func RunInNewConsole(exe string, args, env []string, dir string) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// ProcessCreated returns a process's creation time, or zero if it is gone.
func ProcessCreated(pid int) time.Time {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 {
		return time.Time{}
	}
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, creation.Nanoseconds())
}
