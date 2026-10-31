package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconPNG draws the app icon (the O of the Media Core wordmark: a ring around
// a solid core, proportions from android/brand/mark.svg) at n×n, so the
// home-screen icons need no image files or build tools.
func iconPNG(n int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	bg := [3]float64{0x0e, 0x10, 0x14}
	ink := [3]float64{0xf4, 0xf5, 0xf8}
	core := [3]float64{0x1e, 0x3a, 0xb8}
	const ss = 8 // supersampling for smooth edges
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var sum [3]float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(x) + (float64(sx)+0.5)/ss) / float64(n)
					v := (float64(y) + (float64(sy)+0.5)/ss) / float64(n)
					c := mark(u, v, bg, ink, core)
					for i := range sum {
						sum[i] += c[i]
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(sum[0]/ss/ss + 0.5), uint8(sum[1]/ss/ss + 0.5), uint8(sum[2]/ss/ss + 0.5), 0xff})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// mark picks the colour at (u,v) in the unit square. Sized for mark.svg's 108
// canvas, with room around the ring for phones that crop icons to a circle.
func mark(u, v float64, bg, ink, core [3]float64) [3]float64 {
	d := math.Hypot(u-0.5, v-0.5) * 108
	switch {
	case d <= 9.63:
		return core
	case d >= 25.62-4.375 && d <= 25.62+4.375:
		return ink
	}
	return bg
}
