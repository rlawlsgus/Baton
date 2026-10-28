package fsx

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	fsctlSetReparsePoint    = 0x000900A4
	ioReparseTagMountPoint  = 0xA0000003
	reparseMountPointHeader = 8 // four uint16 offsets/lengths after the common header
)

// CreateJunction makes link an NTFS directory junction to target. Junctions
// need no admin rights or developer mode (unlike symlinks). It refuses to
// replace anything that already exists at link.
func CreateJunction(link, target string) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	fi, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errors.New("junction target is not a directory: " + target)
	}
	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	if err := setMountPoint(link, target); err != nil {
		os.Remove(link)
		return err
	}
	return nil
}

func setMountPoint(link, target string) error {
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	sub := utf16.Encode([]rune(`\??\` + target))
	prn := utf16.Encode([]rune(target))
	subBytes, prnBytes := len(sub)*2, len(prn)*2
	pathBuf := subBytes + 2 + prnBytes + 2 // both names NUL-terminated
	dataLen := reparseMountPointHeader + pathBuf
	buf := make([]byte, 8+dataLen)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], ioReparseTagMountPoint)
	le.PutUint16(buf[4:], uint16(dataLen))
	le.PutUint16(buf[8:], 0)                   // SubstituteNameOffset
	le.PutUint16(buf[10:], uint16(subBytes))   // SubstituteNameLength
	le.PutUint16(buf[12:], uint16(subBytes+2)) // PrintNameOffset
	le.PutUint16(buf[14:], uint16(prnBytes))   // PrintNameLength
	off := 16
	for _, c := range sub {
		le.PutUint16(buf[off:], c)
		off += 2
	}
	off += 2
	for _, c := range prn {
		le.PutUint16(buf[off:], c)
		off += 2
	}
	var returned uint32
	return windows.DeviceIoControl(h, fsctlSetReparsePoint, &buf[0], uint32(len(buf)), nil, 0, &returned, nil)
}

// JunctionTarget returns where a junction points, or "" if path is not one.
func JunctionTarget(path string) string {
	fi, err := os.Lstat(path)
	if err != nil || !IsReparsePoint(fi) {
		return ""
	}
	t, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if len(t) > 4 && t[:4] == `\??\` {
		t = t[4:]
	}
	return t
}
