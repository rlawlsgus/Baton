// Package winproc runs claude.exe on baton's own console and stops it the way
// Phase 0 showed to be clean: CTRL_BREAK to its process group, with a Job
// Object only as the last-resort kill.
package winproc

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleCP          = kernel32.NewProc("GetConsoleCP")
	procSetConsoleCP          = kernel32.NewProc("SetConsoleCP")
	procGetConsoleOutputCP    = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP    = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

// ErrNoConsole means baton is not attached to a real console (for example
// mintty without ConPTY, PowerShell ISE, or redirected stdin). claude.exe
// refuses to run interactively there.
var ErrNoConsole = errors.New("not attached to a console window")

// ConsoleState is the console baton started with. claude restores the console
// to whatever it inherited, so after a kill (which skips that) dirty state
// would carry into every later run unless baton restores its own baseline.
type ConsoleState struct {
	in, out           windows.Handle
	inMode, outMode   uint32
	inputCP, outputCP uintptr
}

func openConsole(name string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

// SaveConsole captures input/output modes and code pages.
func SaveConsole() (*ConsoleState, error) {
	in, err := openConsole("CONIN$")
	if err != nil {
		return nil, ErrNoConsole
	}
	out, err := openConsole("CONOUT$")
	if err != nil {
		windows.CloseHandle(in)
		return nil, ErrNoConsole
	}
	s := &ConsoleState{in: in, out: out}
	if windows.GetConsoleMode(in, &s.inMode) != nil || windows.GetConsoleMode(out, &s.outMode) != nil {
		s.Close()
		return nil, ErrNoConsole
	}
	s.inputCP, _, _ = procGetConsoleCP.Call()
	s.outputCP, _, _ = procGetConsoleOutputCP.Call()
	return s, nil
}

// Restore puts modes and code pages back to the saved baseline.
func (s *ConsoleState) Restore() {
	if s == nil {
		return
	}
	_ = windows.SetConsoleMode(s.in, s.inMode)
	_ = windows.SetConsoleMode(s.out, s.outMode)
	if s.inputCP != 0 {
		procSetConsoleCP.Call(s.inputCP)
	}
	if s.outputCP != 0 {
		procSetConsoleOutputCP.Call(s.outputCP)
	}
}

// vtReset undoes the terminal modes claude's TUI turns on: alternate screen,
// hidden cursor, bracketed paste, focus/theme reports, mouse tracking, kitty
// keyboard and modifyOtherKeys. claude emits this itself on a clean exit;
// baton writes it when claude could not (killed, crashed, stopped early).
// ESC 7 saves the cursor first, so "?1049l" (which restores the saved cursor)
// cannot jump the cursor when the terminal was already on the main screen.
const vtReset = "\x1b7\x1b[?1049l\x1b[?25h\x1b[?2004l\x1b[?1004l\x1b[?2031l" +
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1016l\x1b[<u\x1b[>4m\x1b[0m"

// ResetTerminal writes vtReset with VT processing enabled for the write.
func (s *ConsoleState) ResetTerminal() {
	if s == nil {
		return
	}
	var mode uint32
	if windows.GetConsoleMode(s.out, &mode) == nil {
		_ = windows.SetConsoleMode(s.out, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
	}
	var n uint32
	b := []byte(vtReset)
	_ = windows.WriteFile(s.out, b, &n, nil)
	_ = windows.SetConsoleMode(s.out, mode)
}

// FlushInput drops keystrokes queued while no claude was running; a new
// claude discards them anyway, and stray keys could answer a prompt.
func (s *ConsoleState) FlushInput() {
	if s != nil {
		_ = windows.FlushConsoleInputBuffer(s.in)
	}
}

func (s *ConsoleState) Close() {
	if s.in != 0 {
		windows.CloseHandle(s.in)
	}
	if s.out != 0 {
		windows.CloseHandle(s.out)
	}
}

// EnableCtrlC undoes an inherited "ignore Ctrl+C" (set for a process started
// in a new process group, and passed on to its children). Without it Ctrl+C
// would only end a console read, never reach the process as an interrupt.
func EnableCtrlC() {
	_, _, _ = procSetConsoleCtrlHandler.Call(0, 0)
}

// StdinIsConsole is a cheap check used before prompting the user.
func StdinIsConsole() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}

// AdoptConsoleStdio points os.Stdin/Stdout/Stderr (and the process std
// handles that children inherit) at this process's console. A process started
// with CREATE_NEW_CONSOLE through os/exec gets NUL std handles, not its window.
func AdoptConsoleStdio() error {
	in, err := openConsole("CONIN$")
	if err != nil {
		return ErrNoConsole
	}
	out, err := openConsole("CONOUT$")
	if err != nil {
		windows.CloseHandle(in)
		return ErrNoConsole
	}
	_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, in)
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, out)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, out)
	os.Stdin = os.NewFile(uintptr(in), "CONIN$")
	os.Stdout = os.NewFile(uintptr(out), "CONOUT$")
	os.Stderr = os.Stdout
	return nil
}
