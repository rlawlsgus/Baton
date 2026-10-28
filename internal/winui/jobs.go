package winui

import (
	"bytes"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Jobs runs the GUI's hidden baton.exe commands, each in a job object of its
// own: when a command times out, or the GUI exits, the command and every
// process it started (claude auth status, logout) end with it.
type Jobs struct {
	mu   sync.Mutex
	open map[windows.Handle]bool
}

// Run runs exe hidden and returns its combined output and exit code.
func (j *Jobs) Run(exe string, args, env []string, timeout time.Duration) (string, int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.SysProcAttr = HiddenProcAttr()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return "", -1, err
	}
	job := killOnCloseJob(cmd.Process.Pid)
	if job != 0 {
		j.mu.Lock()
		if j.open == nil {
			j.open = map[windows.Handle]bool{}
		}
		j.open[job] = true
		j.mu.Unlock()
	}
	timer := time.AfterFunc(timeout, func() {
		if job == 0 {
			_ = cmd.Process.Kill()
			return
		}
		// CloseAll may have closed (and Windows reused) the handle already.
		j.mu.Lock()
		if j.open[job] {
			_ = windows.TerminateJobObject(job, 1)
		}
		j.mu.Unlock()
	})
	err := cmd.Wait()
	timer.Stop()
	if job != 0 {
		j.mu.Lock()
		if j.open[job] {
			delete(j.open, job)
			windows.CloseHandle(job)
		}
		j.mu.Unlock()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.String(), -1, err
	}
	return out.String(), 0, nil
}

// CloseAll ends every command still running.
func (j *Jobs) CloseAll() {
	j.mu.Lock()
	defer j.mu.Unlock()
	for h := range j.open {
		windows.CloseHandle(h) // KILL_ON_JOB_CLOSE
	}
	j.open = nil
}

func killOnCloseJob(pid int) windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
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

// StartConsole starts the command mk builds in a console window of its own
// that outlives the GUI, even when the GUI itself runs inside a job (a
// launcher's). It returns the command that was started.
func StartConsole(mk func() *exec.Cmd) (*exec.Cmd, error) {
	LetOthersComeForward() // the new window must not open behind baton's
	return startWithFallback(mk, windows.CREATE_NEW_CONSOLE)
}

// startWithFallback starts a process that leaves this one's job, or stays in
// it when the job does not allow that. An exec.Cmd can only be started once,
// so mk builds a fresh one for each attempt.
func startWithFallback(mk func() *exec.Cmd, flags uint32) (*exec.Cmd, error) {
	attempt := func(f uint32) (*exec.Cmd, error) {
		cmd := mk()
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.CreationFlags |= f
		return cmd, cmd.Start()
	}
	cmd, err := attempt(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		cmd, err = attempt(flags)
	}
	if err != nil {
		return nil, err
	}
	return cmd, nil
}
