package pack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// ICO writes a Windows .ico from a square image at the given sizes (≤256). PNG entries (Windows Vista and later), or
// classic 32-bit bitmaps when bmp: NSIS's installer icon needs those (makensis aborts on PNG entries).
func ICO(src image.Image, sizes []int, bmp bool) []byte {
	var imgs [][]byte
	for _, n := range sizes {
		img := Scale(src, n)
		if bmp {
			imgs = append(imgs, dib(img))
		} else {
			imgs = append(imgs, encodePNG(img))
		}
	}
	var out bytes.Buffer
	w := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	w(uint16(0))
	w(uint16(1)) // icon
	w(uint16(len(imgs)))
	offset := 6 + 16*len(imgs)
	for i, b := range imgs {
		dim := uint8(sizes[i])
		if sizes[i] >= 256 {
			dim = 0 // 256 is written as 0
		}
		w(dim)
		w(dim)
		w(uint8(0))
		w(uint8(0))
		w(uint16(1))
		w(uint16(32))
		w(uint32(len(b)))
		w(uint32(offset))
		offset += len(b)
	}
	for _, b := range imgs {
		out.Write(b)
	}
	return out.Bytes()
}

// dib: an icon entry as a bitmap: BITMAPINFOHEADER (height doubled: colour, then mask), BGRA rows bottom-up, then
// the 1-bit AND mask (all zero: alpha decides).
func dib(img image.Image) []byte {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	var b bytes.Buffer
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	put(uint32(40))
	put(int32(w))
	put(int32(h * 2))
	put(uint16(1))
	put(uint16(32))
	put(uint32(0))
	put(uint32(w * h * 4))
	put(int32(0))
	put(int32(0))
	put(uint32(0))
	put(uint32(0))
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, ((w+31)/32)*4*h))
	return b.Bytes()
}
