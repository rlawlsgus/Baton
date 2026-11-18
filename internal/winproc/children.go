package winproc

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Proc is a running process: its id and its program's file name.
type Proc struct {
	PID int
	Exe string // the file name only (bash.exe)
}

// Children are the processes pid started that run now. A process whose
// parent's id is pid but that is older than pid was started by an earlier
// process that had the same id, and is not one of them.
func Children(pid int) []Proc {
	born := ProcessCreated(pid)
	if born.IsZero() {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []Proc
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ParentProcessID) != pid || int(e.ProcessID) == pid {
			continue
		}
		if at := ProcessCreated(int(e.ProcessID)); at.IsZero() || at.Before(born) {
			continue
		}
		out = append(out, Proc{PID: int(e.ProcessID), Exe: windows.UTF16ToString(e.ExeFile[:])})
	}
	return out
}

// CommandLine is the command line process pid was started with, as
// Windows keeps it (Windows 8.1 and later).
func CommandLine(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	size := uint32(4096)
	for try := 0; try < 4; try++ {
		buf := make([]byte, size)
		var need uint32
		err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), size, &need)
		if err == nil {
			s := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
			if s.Length == 0 || s.Buffer == nil {
				return "", nil
			}
			u := unsafe.Slice(s.Buffer, s.Length/2)
			return windows.UTF16ToString(u), nil
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) && !errors.Is(err, windows.STATUS_BUFFER_TOO_SMALL) &&
			!errors.Is(err, windows.STATUS_BUFFER_OVERFLOW) || need <= size {
			return "", err
		}
		size = need
	}
	return "", errors.New("command line keeps growing")
}

// IsConsoleHost reports whether exe is the console host Windows starts for
// a console program (conhost.exe): it belongs to that program, and runs
// nothing of its own.
func IsConsoleHost(exe string) bool { return strings.EqualFold(exe, "conhost.exe") }
