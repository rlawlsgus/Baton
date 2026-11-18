package recycle

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// testPrefix starts the name of everything the real-bin test makes. Only an
// item whose bin record says it came from such a name is ever taken out of
// the bin again.
const testPrefix = "baton-recycle-test-"

// TestRealRecycleBin puts a small file and a small folder in the user's
// real Recycle Bin, so it runs only with BATON_TEST_RECYCLE=1. It makes
// them in a new folder under BATON_TEST_RECYCLE_DIR (or the test's own
// temporary folder), and afterwards takes exactly those out of the bin
// again, unless BATON_TEST_RECYCLE_KEEP=1 asks to leave them there to look
// at.
func TestRealRecycleBin(t *testing.T) {
	if os.Getenv("BATON_TEST_RECYCLE") != "1" {
		t.Skip("set BATON_TEST_RECYCLE=1 to use the real Recycle Bin")
	}
	base := os.Getenv("BATON_TEST_RECYCLE_DIR")
	if base == "" {
		base = t.TempDir()
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "check-")
	if err != nil {
		t.Fatal(err)
	}
	tag := make([]byte, 4)
	rand.Read(tag)
	name := testPrefix + hex.EncodeToString(tag)
	file := filepath.Join(dir, name+".txt")
	folder := filepath.Join(dir, name+"-dir")
	write(t, file, "baton recycle test")
	write(t, filepath.Join(folder, "inside.txt"), "baton recycle test")

	b, landed := realBin()
	if os.Getenv("BATON_TEST_RECYCLE_KEEP") != "1" {
		defer func() {
			for _, to := range landed {
				takeOut(t, to)
			}
		}()
	} else {
		defer t.Log("left in the Recycle Bin (BATON_TEST_RECYCLE_KEEP=1)")
	}
	if err := b.Preflight([]string{file, folder}); err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	// Held open without FILE_SHARE_DELETE: tried a few times, then refused,
	// with nothing moved.
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := b.Recycle([]string{file, folder})
	f.Close()
	if moved != 0 || !errors.Is(err, ErrInUse) || errors.Is(err, ErrPartial) || !exists(file) || !exists(folder) {
		t.Fatalf("Recycle of a file in use = %d, %v; want 0 and in use, both still there", moved, err)
	}

	moved, err = b.Recycle([]string{file, folder})
	if err != nil || moved != 2 {
		t.Fatalf("Recycle = %d, %v; want 2, nil", moved, err)
	}
	if exists(file) || exists(folder) {
		t.Fatal("the items are still in place")
	}
	checkLanded(t, file, landed[file])
	checkLanded(t, folder, landed[folder])
	if data, err := os.ReadFile(filepath.Join(landed[folder], "inside.txt")); err != nil || string(data) != "baton recycle test" {
		t.Fatalf("the folder's file in the bin: %q, %v", data, err)
	}

	// A folder with a path longer than MAX_PATH inside: Preflight refuses
	// it, and the shell, asked anyway, would delete it for good, which the
	// progress sink cancels.
	deep := filepath.Join(dir, name+"-deep")
	inner := filepath.Join(deep, strings.Repeat("x", max(1, maxPath-utf16Len(longPath(deep))))+".txt")
	write(t, inner, "baton recycle test")
	if _, err := b.Recycle([]string{deep}); !errors.Is(err, ErrTooLong) {
		t.Fatalf("Recycle of a folder too deep = %v; want too long", err)
	}
	if serr := onSTA(func() { err = b.move(deep) }); serr != nil {
		t.Fatal(serr)
	}
	switch {
	case errors.Is(err, ErrUnavailable) && exists(inner):
	case err == nil && landed[deep] != "":
		t.Logf("this Windows recycles a folder with a path too long inside")
		checkLanded(t, deep, landed[deep])
	default:
		t.Fatalf("the shell asked to recycle a folder too deep: %v (still there: %v)", err, exists(inner))
	}
}

// realBin is the real Recycle Bin, telling where each item lands.
func realBin() (*shellBin, map[string]string) {
	landed := map[string]string{}
	return &shellBin{facts: osFacts(), landed: func(from, to string) { landed[from] = to }}, landed
}

// checkLanded checks that the bin has the item from at to, and that its
// record names from as where it came from.
func checkLanded(t *testing.T, from, to string) {
	t.Helper()
	t.Logf("%s is in the bin as %s", from, to)
	if !isBinItem(to) || !exists(to) {
		t.Fatalf("%s: not found in the bin (%q)", from, to)
	}
	orig, err := originalPath(infoFile(to))
	if err != nil {
		t.Fatalf("%s: %v", infoFile(to), err)
	}
	if !strings.EqualFold(orig, longPath(from)) && !strings.EqualFold(orig, from) {
		t.Fatalf("the bin says %s came from %s", to, orig)
	}
}

// takeOut removes one test item from the bin: its $R item and its $I
// record, and nothing that did not come from a name the test made.
func takeOut(t *testing.T, to string) {
	t.Helper()
	orig, err := originalPath(infoFile(to))
	if !isBinItem(to) || err != nil || !strings.HasPrefix(filepath.Base(orig), testPrefix) {
		t.Errorf("not taking %s out of the bin: it is not the test's (%q, %v)", to, orig, err)
		return
	}
	if err := os.RemoveAll(to); err != nil {
		t.Errorf("taking %s out of the bin: %v", to, err)
		return
	}
	if err := os.Remove(infoFile(to)); err != nil {
		t.Errorf("taking %s out of the bin: %v", infoFile(to), err)
	}
}

// isBinItem: a $R name in a $Recycle.Bin folder.
func isBinItem(p string) bool {
	return strings.HasPrefix(filepath.Base(p), "$R") &&
		strings.EqualFold(filepath.Base(filepath.Dir(filepath.Dir(p))), "$Recycle.Bin")
}

// infoFile is the $I record next to a $R item.
func infoFile(item string) string {
	return filepath.Join(filepath.Dir(item), "$I"+strings.TrimPrefix(filepath.Base(item), "$R"))
}

// originalPath reads where a bin item came from out of its $I record:
// version 2 (Windows 10 and later) holds a length and the path, version 1 a
// fixed MAX_PATH field.
func originalPath(info string) (string, error) {
	data, err := os.ReadFile(info)
	if err != nil {
		return "", err
	}
	if len(data) < 28 {
		return "", errors.New("short $I record")
	}
	var units []uint16
	switch binary.LittleEndian.Uint64(data) {
	case 2:
		n := int(binary.LittleEndian.Uint32(data[24:]))
		if len(data) < 28+2*n {
			return "", errors.New("short $I record")
		}
		for i := range n {
			units = append(units, binary.LittleEndian.Uint16(data[28+2*i:]))
		}
	case 1:
		for i := 24; i+1 < len(data); i += 2 {
			units = append(units, binary.LittleEndian.Uint16(data[i:]))
		}
	default:
		return "", errors.New("unknown $I version")
	}
	s := string(utf16.Decode(units))
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return s, nil
}
