package pack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"strings"
	"testing"
)

func square(n int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.SetNRGBA(x, y, color.NRGBA{200, 50, 50, 255})
		}
	}
	return img
}

func TestScale(t *testing.T) {
	img := Scale(square(512), 16)
	if img.Bounds().Dx() != 16 || img.NRGBAAt(8, 8) != (color.NRGBA{200, 50, 50, 255}) {
		t.Fatalf("scaled: %v %v", img.Bounds(), img.NRGBAAt(8, 8))
	}
}

func TestICNS(t *testing.T) {
	b := ICNS(square(512))
	if string(b[:4]) != "icns" || int(binary.BigEndian.Uint32(b[4:8])) != len(b) {
		t.Fatalf("header: %q %d of %d", b[:4], binary.BigEndian.Uint32(b[4:8]), len(b))
	}
	// walk the entries
	n := 0
	for off := 8; off < len(b); n++ {
		size := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		if !bytes.HasPrefix(b[off+8:], []byte("\x89PNG")) {
			t.Fatalf("entry %q isn't a PNG", b[off:off+4])
		}
		off += size
	}
	if n != 11 {
		t.Fatalf("%d entries", n)
	}
}

func TestICO(t *testing.T) {
	for _, bmp := range []bool{false, true} {
		b := ICO(square(256), []int{16, 32, 256}, bmp)
		if binary.LittleEndian.Uint16(b[2:4]) != 1 || binary.LittleEndian.Uint16(b[4:6]) != 3 {
			t.Fatalf("bmp=%v header %v", bmp, b[:6])
		}
		if b[6+2*16] != 0 { // the 256px entry's width is written as 0
			t.Fatalf("bmp=%v: 256 written as %d", bmp, b[6+2*16])
		}
	}
}

func TestPlist(t *testing.T) {
	p := string(Plist(map[string]any{"CFBundleName": "A & B", "LSUIElement": true}))
	if !strings.Contains(p, "<string>A &amp; B</string>") || !strings.Contains(p, "<key>LSUIElement</key><true/>") {
		t.Fatal(p)
	}
}

func TestFourPart(t *testing.T) {
	for in, want := range map[string]string{"1.2.3": "1.2.3.0", "v1.2.3-4-gabc": "1.2.3.4", "dev": "0.0.0.0"} {
		if got := fourPart(in); got != want {
			t.Errorf("fourPart(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNSIS(t *testing.T) {
	s, err := NSISScript(NSIS{Name: "Notes", Version: "1.0.0", Exe: "Notes.exe", Files: []string{"/x/Notes.exe"}, Out: "/x/setup.exe", RegKey: "Notes"})
	if err != nil || !strings.Contains(s, `File "/x/Notes.exe"`) || !strings.Contains(s, `$LOCALAPPDATA\Programs\Notes`) {
		t.Fatalf("%v\n%s", err, s)
	}
}
