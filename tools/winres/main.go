// Command winres writes cmd/baton/rsrc_windows_{amd64,arm64}.syso: the app
// icon, the application manifest and version information, as COFF objects
// the Go linker adds to baton.exe and baton-app.exe. Standard library only.
//
//	go run ./tools/winres -version 0.1.0
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

func main() {
	version := flag.String("version", "0.1.0", "version to record (major.minor.patch)")
	out := flag.String("out", "cmd/baton", "folder for the .syso files")
	flag.Parse()
	v := parseVersion(*version)

	res := []resource{}
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	group := []byte{0, 0, 1, 0}
	group = binary.LittleEndian.AppendUint16(group, uint16(len(sizes)))
	for i, s := range sizes {
		img := drawIcon(s)
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			fail(err)
		}
		id := uint16(i + 1)
		res = append(res, resource{typ: rtIcon, id: id, data: b.Bytes()})
		w := byte(s)
		if s >= 256 {
			w = 0
		}
		group = append(group, w, w, 0, 0)
		group = binary.LittleEndian.AppendUint16(group, 1)  // planes
		group = binary.LittleEndian.AppendUint16(group, 32) // bit count
		group = binary.LittleEndian.AppendUint32(group, uint32(b.Len()))
		group = binary.LittleEndian.AppendUint16(group, id)
	}
	res = append(res,
		resource{typ: rtGroupIcon, id: 1, data: group},
		resource{typ: rtVersion, id: 1, data: versionInfo(v, *version)},
		resource{typ: rtManifest, id: 1, data: []byte(manifest(v))},
	)
	for arch, machine := range map[string]uint16{"amd64": 0x8664, "arm64": 0xAA64} {
		reloc := uint16(3) // IMAGE_REL_AMD64_ADDR32NB
		if arch == "arm64" {
			reloc = 2 // IMAGE_REL_ARM64_ADDR32NB
		}
		p := filepath.Join(*out, "rsrc_windows_"+arch+".syso")
		if err := os.WriteFile(p, coff(machine, reloc, res), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", p)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "winres:", err)
	os.Exit(1)
}

func parseVersion(s string) [4]uint16 {
	var v [4]uint16
	core, _, _ := strings.Cut(strings.TrimPrefix(s, "v"), "-")
	for i, p := range strings.SplitN(core, ".", 4) {
		n, err := strconv.Atoi(p)
		if err != nil {
			fail(fmt.Errorf("bad version %q", s))
		}
		v[i] = uint16(n)
	}
	return v
}

// ---- the icon -----------------------------------------------------------------------

// drawIcon paints baton's logo (a rounded square with two passing batons)
// at size x size, 4x4 supersampled.
func drawIcon(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	bg := color.NRGBA{0x2b, 0x4c, 0x7e, 0xff}
	cream := color.NRGBA{0xf4, 0xef, 0xe6, 0xff}
	amber := color.NRGBA{0xf2, 0xb5, 0x44, 0xff}
	type shape struct {
		c      color.NRGBA
		inside func(x, y float64) bool
	}
	k := 64.0 // the design is drawn on a 64x64 grid
	capsule := func(x1, y1, x2, y2, r float64) func(x, y float64) bool {
		return func(x, y float64) bool { return segDist(x, y, x1, y1, x2, y2) <= r }
	}
	shapes := []shape{
		{bg, func(x, y float64) bool { return roundRect(x, y, 0, 0, k, k, 14) }},
		{cream, capsule(14, 42, 34, 22, 3.5)},
		{amber, capsule(30, 42, 50, 22, 3.5)},
		{cream, func(x, y float64) bool { return math.Hypot(x-18, y-46) <= 4 }},
		{amber, func(x, y float64) bool { return math.Hypot(x-46, y-18) <= 4 }},
	}
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * k / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) * k / float64(size)
					var c color.NRGBA
					hit := false
					for _, s := range shapes {
						if s.inside(x, y) {
							c, hit = s.c, true
						}
					}
					if hit {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a++
					}
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / (ss * ss))})
			}
		}
	}
	return img
}

func segDist(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	t := ((px-x1)*dx + (py-y1)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

func roundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return math.Hypot(x-cx, y-cy) <= r
}

// ---- manifest and version -------------------------------------------------------------

func manifest(v [4]uint16) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="baton" version="` + fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3]) + `" processorArchitecture="*"/>
  <description>baton</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application><supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/></application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
`
}

// versionInfo builds a VS_VERSIONINFO block.
func versionInfo(v [4]uint16, text string) []byte {
	fixed := make([]byte, 0, 52)
	for _, u := range []uint32{
		0xFEEF04BD, 0x00010000,
		uint32(v[0])<<16 | uint32(v[1]), uint32(v[2])<<16 | uint32(v[3]), // file version
		uint32(v[0])<<16 | uint32(v[1]), uint32(v[2])<<16 | uint32(v[3]), // product version
		0x3F, 0, 0x00040004, 1, 0, 0, 0, // flags mask, flags, VOS_NT_WINDOWS32, VFT_APP, subtype, date
	} {
		fixed = binary.LittleEndian.AppendUint32(fixed, u)
	}
	strs := [][2]string{
		{"CompanyName", "baton"},
		{"FileDescription", "baton - continue Claude Code across your Claude accounts"},
		{"FileVersion", text},
		{"LegalCopyright", "MIT License"},
		{"ProductName", "baton"},
		{"ProductVersion", text},
	}
	var strings_ [][]byte
	for _, kv := range strs {
		val := utf16z(kv[1])
		strings_ = append(strings_, block(kv[0], 1, len(val)/2, val))
	}
	table := block("040904b0", 1, 0, nil, strings_...)
	sfi := block("StringFileInfo", 1, 0, nil, table)
	translation := binary.LittleEndian.AppendUint32(nil, 0x04B00409)
	vfi := block("VarFileInfo", 1, 0, nil, block("Translation", 0, 4, translation))
	return block("VS_VERSION_INFO", 0, len(fixed), fixed, sfi, vfi)
}

// block is one version-resource node: header, key, value and children, each
// aligned to 32 bits.
func block(key string, typ uint16, valueLen int, value []byte, children ...[]byte) []byte {
	b := make([]byte, 6)
	b = append(b, utf16z(key)...)
	b = pad4(b)
	b = append(b, value...)
	for _, c := range children {
		b = pad4(b)
		b = append(b, c...)
	}
	binary.LittleEndian.PutUint16(b[0:], uint16(len(b)))
	binary.LittleEndian.PutUint16(b[2:], uint16(valueLen))
	binary.LittleEndian.PutUint16(b[4:], typ)
	return b
}

func utf16z(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return append(b, 0, 0)
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// ---- COFF -----------------------------------------------------------------------------

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

type resource struct {
	typ, id uint16
	data    []byte
}

// coff lays out a .rsrc section (type -> id -> language directories, data
// entries, then the data) and the relocations that turn each data entry's
// offset into an RVA when linked.
func coff(machine, relocType uint16, res []resource) []byte {
	sort.Slice(res, func(i, j int) bool {
		if res[i].typ != res[j].typ {
			return res[i].typ < res[j].typ
		}
		return res[i].id < res[j].id
	})
	byType := map[uint16][]resource{}
	var types []uint16
	for _, r := range res {
		if len(byType[r.typ]) == 0 {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], r)
	}
	dirSize := func(n int) int { return 16 + 8*n }
	// Offsets: root dir, type dirs, name dirs (one language each), data entries, data.
	off := dirSize(len(types))
	typeDirOff := map[uint16]int{}
	for _, t := range types {
		typeDirOff[t] = off
		off += dirSize(len(byType[t]))
	}
	langDirOff := make([]int, len(res))
	for i := range res {
		langDirOff[i] = off
		off += dirSize(1)
	}
	entryOff := make([]int, len(res))
	for i := range res {
		entryOff[i] = off
		off += 16
	}
	dataOff := make([]int, len(res))
	for i, r := range res {
		off = (off + 7) &^ 7
		dataOff[i] = off
		off += len(r.data)
	}
	sec := make([]byte, (off+7)&^7)
	putDir := func(at, n int) {
		binary.LittleEndian.PutUint16(sec[at+14:], uint16(n)) // NumberOfIdEntries
	}
	putEntry := func(at int, id uint32, target int, isDir bool) {
		binary.LittleEndian.PutUint32(sec[at:], id)
		v := uint32(target)
		if isDir {
			v |= 0x80000000
		}
		binary.LittleEndian.PutUint32(sec[at+4:], v)
	}
	putDir(0, len(types))
	idx := 0
	for ti, t := range types {
		putEntry(16+8*ti, uint32(t), typeDirOff[t], true)
		putDir(typeDirOff[t], len(byType[t]))
		for ri, r := range byType[t] {
			putEntry(typeDirOff[t]+16+8*ri, uint32(r.id), langDirOff[idx], true)
			putDir(langDirOff[idx], 1)
			putEntry(langDirOff[idx]+16, langEnUS, entryOff[idx], false)
			binary.LittleEndian.PutUint32(sec[entryOff[idx]:], uint32(dataOff[idx])) // relocated to an RVA
			binary.LittleEndian.PutUint32(sec[entryOff[idx]+4:], uint32(len(r.data)))
			copy(sec[dataOff[idx]:], r.data)
			idx++
		}
	}
	var relocs []byte
	for i := range res {
		relocs = binary.LittleEndian.AppendUint32(relocs, uint32(entryOff[i])) // VirtualAddress
		relocs = binary.LittleEndian.AppendUint32(relocs, 0)                   // symbol 0: .rsrc
		relocs = binary.LittleEndian.AppendUint16(relocs, relocType)
	}

	const headerSize, sectionHeaderSize = 20, 40
	rawOff := headerSize + sectionHeaderSize
	relocOff := rawOff + len(sec)
	symOff := relocOff + len(relocs)
	var b []byte
	b = binary.LittleEndian.AppendUint16(b, machine)
	b = binary.LittleEndian.AppendUint16(b, 1) // sections
	b = binary.LittleEndian.AppendUint32(b, 0) // timestamp
	b = binary.LittleEndian.AppendUint32(b, uint32(symOff))
	b = binary.LittleEndian.AppendUint32(b, 1) // symbols
	b = binary.LittleEndian.AppendUint16(b, 0) // optional header
	b = binary.LittleEndian.AppendUint16(b, 0) // characteristics
	b = append(b, ".rsrc\x00\x00\x00"...)
	b = binary.LittleEndian.AppendUint32(b, 0) // virtual size
	b = binary.LittleEndian.AppendUint32(b, 0) // virtual address
	b = binary.LittleEndian.AppendUint32(b, uint32(len(sec)))
	b = binary.LittleEndian.AppendUint32(b, uint32(rawOff))
	b = binary.LittleEndian.AppendUint32(b, uint32(relocOff))
	b = binary.LittleEndian.AppendUint32(b, 0) // line numbers
	b = binary.LittleEndian.AppendUint16(b, uint16(len(res)))
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0x40000040) // initialized data, readable
	b = append(b, sec...)
	b = append(b, relocs...)
	// Symbol table: the section symbol, then an empty string table.
	b = append(b, ".rsrc\x00\x00\x00"...)
	b = binary.LittleEndian.AppendUint32(b, 0) // value
	b = binary.LittleEndian.AppendUint16(b, 1) // section number
	b = binary.LittleEndian.AppendUint16(b, 0) // type
	b = append(b, 3, 0)                        // static, no aux symbols
	b = binary.LittleEndian.AppendUint32(b, 4) // string table size
	return b
}
