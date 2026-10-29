// Package notify shows Windows notifications (toasts) for baton: a session
// that switched accounts, needs an answer in its window or finished its work.
//
// The toast goes through the Windows Runtime (combase.dll) by hand, like the
// WebView2 and shell-link code elsewhere in baton: standard library and
// golang.org/x/sys only. Windows shows a desktop program's toasts under an
// AppUserModelID that is registered, so Register writes baton's (its name and
// icon) under HKEY_CURRENT_USER, together with the baton-session: link a click
// on a toast opens.
package notify

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AppID is baton's AppUserModelID: the window's taskbar identity, the Start
// menu entry's, and the name its toasts are shown under.
const AppID = "baton.gui"

var appID = AppID // the id toasts are shown under (a var for tests)

// Scheme is the link a click on a toast opens: baton-session:<run id>, or
// baton-session:open for baton's own window.
const (
	Scheme     = "baton-session"
	OpenWindow = "open"
)

// Note is one toast.
type Note struct {
	Title string
	Body  string
	// Launch is opened when the toast is clicked (a baton-session: link), if set.
	Launch string
	// Tag and Group let a newer toast replace an older one: one per kind of
	// news (Tag) and session (Group). Each at most 64 characters.
	Tag   string
	Group string
}

// testFileEnv names a file that receives toasts as JSON lines instead of the
// screen (the end-to-end tests read it).
const testFileEnv = "BATON_NOTIFY_FILE"

// Show shows a toast. It returns once Windows has it; the toast stays up after
// the program exits.
func Show(n Note) error {
	if p := os.Getenv(testFileEnv); p != "" {
		return appendTestNote(p, n)
	}
	done := make(chan error, 1)
	go func() {
		// The Windows Runtime wants a thread that stays put while it is in use.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- show(n)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("Windows did not take the notification in time")
	}
}

func appendTestNote(path string, n Note) error {
	line, err := json.Marshal(map[string]string{"title": n.Title, "body": n.Body, "launch": n.Launch, "tag": n.Tag, "group": n.Group})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// toastXML is the toast's content: a title, a line of text and, for a click,
// the link to open.
func toastXML(n Note) string {
	var b bytes.Buffer
	b.WriteString(`<toast`)
	if n.Launch != "" {
		b.WriteString(` activationType="protocol" launch="`)
		_ = xml.EscapeText(&b, []byte(n.Launch))
		b.WriteString(`"`)
	}
	b.WriteString(`><visual><binding template="ToastGeneric"><text>`)
	_ = xml.EscapeText(&b, []byte(n.Title))
	b.WriteString(`</text><text>`)
	_ = xml.EscapeText(&b, []byte(n.Body))
	b.WriteString(`</text></binding></visual></toast>`)
	return b.String()
}

// ---- Windows Runtime -------------------------------------------------------

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
)

var (
	iidXMLDocument   = windows.GUID{Data1: 0xF7F3A506, Data2: 0x1E87, Data3: 0x42D6, Data4: [8]byte{0xBC, 0xFB, 0xB8, 0xC8, 0x09, 0xFA, 0x54, 0x94}}
	iidXMLDocumentIO = windows.GUID{Data1: 0x6CD0E74E, Data2: 0xEE65, Data3: 0x4489, Data4: [8]byte{0x9E, 0xBF, 0xCA, 0x43, 0xE8, 0x7B, 0xA6, 0x37}}
	iidToastFactory  = windows.GUID{Data1: 0x04124B20, Data2: 0x82C6, Data3: 0x4229, Data4: [8]byte{0xB1, 0x09, 0xFD, 0x9E, 0xD4, 0x66, 0x2B, 0x53}}
	iidToast2        = windows.GUID{Data1: 0x9DFB9FD1, Data2: 0x143A, Data3: 0x490E, Data4: [8]byte{0x90, 0xBF, 0xB9, 0xFB, 0xA7, 0x13, 0x2D, 0xE7}}
	iidToastManager  = windows.GUID{Data1: 0x50AC103F, Data2: 0xD235, Data3: 0x4598, Data4: [8]byte{0xBB, 0xEF, 0x98, 0xFE, 0x4D, 0x1A, 0x3A, 0xD4}}
)

// Method slots after IUnknown (0-2) and IInspectable (3-5).
const (
	vtQueryInterface      = 0
	vtRelease             = 2
	xmlLoadXML            = 6 // IXmlDocumentIO.LoadXml
	factoryCreate         = 6 // IToastNotificationFactory.CreateToastNotification
	toast2PutTag          = 6 // IToastNotification2.put_Tag
	toast2PutGroup        = 8 // IToastNotification2.put_Group
	managerNotifierWithID = 7 // IToastNotificationManagerStatics.CreateToastNotifierWithId
	notifierShow          = 6 // IToastNotifier.Show
)

const rpcChangedMode = 0x80010106 // RPC_E_CHANGED_MODE: the thread already has another apartment

func show(n Note) error {
	hr, _, _ := procRoInitialize.Call(1) // RO_INIT_MULTITHREADED
	switch {
	case int32(hr) >= 0:
		defer procRoUninitialize.Call()
	case uint32(hr) != rpcChangedMode:
		return hrError("RoInitialize", hr)
	}

	doc, err := activate("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return err
	}
	defer release(doc)
	io, err := query(doc, &iidXMLDocumentIO)
	if err != nil {
		return err
	}
	defer release(io)
	content, err := newHString(toastXML(n))
	if err != nil {
		return err
	}
	defer deleteHString(content)
	if hr := call(io, xmlLoadXML, content); int32(hr) < 0 {
		return hrError("LoadXml", hr)
	}
	xdoc, err := query(doc, &iidXMLDocument)
	if err != nil {
		return err
	}
	defer release(xdoc)

	factory, err := factoryOf("Windows.UI.Notifications.ToastNotification", &iidToastFactory)
	if err != nil {
		return err
	}
	defer release(factory)
	var toast unsafe.Pointer
	if hr := call(factory, factoryCreate, uintptr(xdoc), uintptr(unsafe.Pointer(&toast))); int32(hr) < 0 || toast == nil {
		return hrError("CreateToastNotification", hr)
	}
	defer release(toast)
	if n.Tag != "" || n.Group != "" {
		// Replacing older toasts is a nicety: without it they only pile up.
		if t2, err := query(toast, &iidToast2); err == nil {
			setString(t2, toast2PutTag, n.Tag)
			setString(t2, toast2PutGroup, n.Group)
			release(t2)
		}
	}

	manager, err := factoryOf("Windows.UI.Notifications.ToastNotificationManager", &iidToastManager)
	if err != nil {
		return err
	}
	defer release(manager)
	id, err := newHString(appID)
	if err != nil {
		return err
	}
	defer deleteHString(id)
	var notifier unsafe.Pointer
	if hr := call(manager, managerNotifierWithID, id, uintptr(unsafe.Pointer(&notifier))); int32(hr) < 0 || notifier == nil {
		return hrError("CreateToastNotifierWithId", hr)
	}
	defer release(notifier)
	if hr := call(notifier, notifierShow, uintptr(toast)); int32(hr) < 0 {
		return hrError("Show", hr)
	}
	return nil
}

func setString(obj unsafe.Pointer, slot int, s string) {
	if s == "" {
		return
	}
	if h, err := newHString(s); err == nil {
		call(obj, slot, h)
		deleteHString(h)
	}
}

// call invokes method slot of a COM object. uintptrescapes keeps &x
// arguments alive and in place for the call.
//
//go:uintptrescapes
func call(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func release(obj unsafe.Pointer) { call(obj, vtRelease) }

func query(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	if hr := call(obj, vtQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); int32(hr) < 0 || out == nil {
		return nil, hrError("QueryInterface", hr)
	}
	return out, nil
}

func activate(class string) (unsafe.Pointer, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(h)
	var out unsafe.Pointer
	if hr, _, _ := procRoActivateInstance.Call(h, uintptr(unsafe.Pointer(&out))); int32(hr) < 0 || out == nil {
		return nil, hrError("RoActivateInstance "+class, hr)
	}
	return out, nil
}

func factoryOf(class string, iid *windows.GUID) (unsafe.Pointer, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(h)
	var out unsafe.Pointer
	if hr, _, _ := procRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); int32(hr) < 0 || out == nil {
		return nil, hrError("RoGetActivationFactory "+class, hr)
	}
	return out, nil
}

func newHString(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	runtime.KeepAlive(u)
	if int32(hr) < 0 {
		return 0, hrError("WindowsCreateString", hr)
	}
	return h, nil
}

func deleteHString(h uintptr) {
	if h != 0 {
		procWindowsDeleteString.Call(h)
	}
}

func hrError(what string, hr uintptr) error {
	return fmt.Errorf("%s: 0x%08X", what, uint32(hr))
}
