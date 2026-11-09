package pack

import (
	"bytes"
	"encoding/binary"
	"image"
)

// ICNS writes a macOS .icns from a square image: PNG entries at the sizes Finder, the Dock and Retina screens ask
// for (macOS 10.7 and later read PNG entries), so no iconutil is needed.
func ICNS(src image.Image) []byte {
	entries := []struct {
		typ  string
		size int
	}{
		{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
		{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512},
	}
	var body bytes.Buffer
	cache := map[int][]byte{}
	for _, e := range entries {
		png, ok := cache[e.size]
		if !ok {
			png = encodePNG(Scale(src, e.size))
			cache[e.size] = png
		}
		body.WriteString(e.typ)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(png)))
		body.Write(png)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}
