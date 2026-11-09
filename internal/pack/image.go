// Package pack writes what packaging needs without the platform's tools: icons (.icns, .ico), Info.plist, the NSIS
// installer script. Used by the macOS engine at runtime (the engine copy's icon) and by the lightron command.
package pack

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// DecodePNG reads a PNG.
func DecodePNG(b []byte) (image.Image, error) { return png.Decode(bytes.NewReader(b)) }

// Scale draws src at n×n, each target pixel the average of the source pixels it covers (alpha-weighted, so
// transparent corners don't darken the edges). Downscaling only, which is what icons need.
func Scale(src image.Image, n int) *image.NRGBA {
	r := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		y0, y1 := r.Min.Y+y*r.Dy()/n, r.Min.Y+(y+1)*r.Dy()/n
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < n; x++ {
			x0, x1 := r.Min.X+x*r.Dx()/n, r.Min.X+(x+1)*r.Dx()/n
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, as, cnt float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					a := float64(c.A)
					rs += float64(c.R) * a
					gs += float64(c.G) * a
					bs += float64(c.B) * a
					as += a
					cnt++
				}
			}
			if as > 0 {
				out.SetNRGBA(x, y, color.NRGBA{uint8(rs / as), uint8(gs / as), uint8(bs / as), uint8(as / cnt)})
			}
		}
	}
	return out
}

// PNG encodes img.
func PNG(img image.Image) []byte { return encodePNG(img) }

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
