package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconPNG draws the app icon (green marquee sign on cream) at n×n, so
// the home-screen icons need no image files or build tools.
func iconPNG(n int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	bg := color.NRGBA{0xf2, 0xf3, 0xed, 0xff}
	fg := color.NRGBA{0x1e, 0x79, 0x56, 0xff}
	const ss = 4 // supersampling for smooth edges
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			hits := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(x) + (float64(sx)+0.5)/ss) / float64(n)
					v := (float64(y) + (float64(sy)+0.5)/ss) / float64(n)
					if marquee(u, v) {
						hits++
					}
				}
			}
			a := float64(hits) / ss / ss
			img.SetNRGBA(x, y, color.NRGBA{
				uint8(float64(bg.R)*(1-a) + float64(fg.R)*a),
				uint8(float64(bg.G)*(1-a) + float64(fg.G)*a),
				uint8(float64(bg.B)*(1-a) + float64(fg.B)*a), 0xff})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// marquee: the theatre sign over a door (peaked top, two rows of bulbs, a title
// bar), in unit coordinates of a full-bleed tile. Same shape as web/icon.svg's
// 24-unit grid, shrunk to stay inside the maskable safe zone (iOS and Android
// round the corners themselves).
func marquee(u, v float64) bool {
	const k = 0.85 // shrink for the safe zone
	x, y := 12+(u-0.5)*24/k, 12+(v-0.5)*24/k
	const hw = 0.75 // half the line width
	seg := func(ax, ay, bx, by float64) bool {
		t := ((x-ax)*(bx-ax) + (y-ay)*(by-ay)) / ((bx-ax)*(bx-ax) + (by-ay)*(by-ay))
		t = min(max(t, 0), 1)
		return math.Hypot(x-ax-t*(bx-ax), y-ay-t*(by-ay)) < hw
	}
	if seg(8.5, 8.75, 12, 5.25) || seg(12, 5.25, 15.5, 8.75) || seg(8.5, 13.75, 15.5, 13.75) {
		return true
	}
	// sign outline: rounded box centred at (12, 13.75), 16.5 x 10, corner radius 1.5
	const r = 1.5
	qx, qy := math.Abs(x-12)-8.25+r, math.Abs(y-13.75)-5+r
	if d := math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r; math.Abs(d) < hw {
		return true
	}
	for _, bx := range []float64{7, 9.5, 12, 14.5, 17} {
		if math.Hypot(x-bx, y-11.25) < hw || math.Hypot(x-bx, y-16.25) < hw {
			return true
		}
	}
	return false
}
