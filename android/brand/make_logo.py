#!/usr/bin/env python3
"""Draws the Media Core logo from Montserrat's outlines.

Usage: PYTHONPATH=<fonttools> make_logo.py <Montserrat[wght].ttf>
Writes wordmark.svg and mark.svg next to this file, and the TV banner. The font isn't kept here; get it from
google/fonts. Needs fonttools and Pillow.

The wordmark is fitted to a 320 x 180 grid, the size the launcher shows: baselines, cap heights and straight stems
land on whole pixels at that size and at the 640 x 360 we ship, which is what keeps it sharp on the TV.
"""
import os
import sys
from fontTools.pens.basePen import BasePen
from fontTools.pens.boundsPen import BoundsPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont
from PIL import Image, ImageChops, ImageDraw

W, H = 320, 180
SCALE = 2           # shipped pixels per grid pixel
SS = 8              # supersampling
BG, INK, CORE = "#0E1014", "#F4F5F8", "#1E3AB8"

BIG_CAP, BIG_STEM, BIG_TRACK = 46, 7, 17      # CORE, in grid pixels
SMALL_CAP, SMALL_STEM, SMALL_TRACK = 15, 2, 7 # MEDIA
GAP = 18
OVERSHOOT = 1       # the O stands this far past the cap and baseline, like a round letter should
CORE_RATIO = 1.1    # core radius, in stems

HERE = os.path.dirname(os.path.abspath(__file__))
BANNER = os.path.join(HERE, "../app/src/main/res/drawable-xhdpi/banner.png")


class Outline(BasePen):
    """Keeps a glyph as SVG path commands and as flattened contours."""

    def __init__(self, glyphs, scale, x, y, sx=None):
        super().__init__(glyphs)
        self.s, self.x, self.y, self.sx = scale, x, y, sx or scale
        self.svg, self.contours = [], []

    def _pt(self, p):
        return (self.x + p[0] * self.sx, self.y - p[1] * self.s)

    def _fmt(self, p):
        return "%g,%g" % tuple(round(v, 3) for v in self._pt(p))

    def _moveTo(self, p):
        self.svg.append("M" + self._fmt(p))
        self.contours.append([self._pt(p)])

    def _lineTo(self, p):
        self.svg.append("L" + self._fmt(p))
        self.contours[-1].append(self._pt(p))

    def _qCurveToOne(self, c, p):
        self.svg.append("Q" + self._fmt(c) + " " + self._fmt(p))
        a, b, d = self._getCurrentPoint(), c, p
        for i in range(1, 25):
            t = i / 24
            self.contours[-1].append(self._pt(tuple((1 - t) ** 2 * a[k] + 2 * t * (1 - t) * b[k] + t * t * d[k] for k in (0, 1))))

    def _curveToOne(self, c1, c2, p):
        self.svg.append("C" + self._fmt(c1) + " " + self._fmt(c2) + " " + self._fmt(p))
        a = self._getCurrentPoint()
        for i in range(1, 25):
            t = i / 24
            self.contours[-1].append(self._pt(tuple(
                (1 - t) ** 3 * a[k] + 3 * t * (1 - t) ** 2 * c1[k] + 3 * t * t * (1 - t) * c2[k] + t ** 3 * p[k] for k in (0, 1))))

    def _closePath(self):
        self.svg.append("Z")


def weight_for(path, stem_per_cap):
    """The weight whose I stem is this fraction of the cap height."""
    full = TTFont(path)
    cap = full["OS/2"].sCapHeight
    lo, hi = 100.0, 900.0
    for _ in range(14):
        mid = (lo + hi) / 2
        pen = BoundsPen(None)
        full.getGlyphSet(location={"wght": mid})["I"].draw(pen)
        if (pen.bounds[2] - pen.bounds[0]) / cap < stem_per_cap:
            lo = mid
        else:
            hi = mid
    return (lo + hi) / 2


def face(path, stem, cap_px):
    w = weight_for(path, stem / cap_px)
    f = instantiateVariableFont(TTFont(path), {"wght": w})
    return f, cap_px / f["OS/2"].sCapHeight, w


def extents(f, c):
    g = f["glyf"][f.getBestCmap()[ord(c)]]
    return g.xMin, g.xMax


def word(f, scale, text, track, baseline, x=None):
    """Lays the letters out with their ink on whole pixels. x None measures only."""
    pen_x = x or 0
    shapes = []
    for c in text:
        lo, hi = extents(f, c)
        if c == "O":
            shapes.append(("O", pen_x))
            pen_x += BIG_CAP + 2 * OVERSHOOT + track
            continue
        ink = round((hi - lo) * scale)  # stretch by under a pixel so the right edge lands on the grid too
        out = Outline(f.getGlyphSet(), scale, pen_x - lo * ink / (hi - lo), baseline, ink / (hi - lo))
        f.getGlyphSet()[f.getBestCmap()[ord(c)]].draw(out)
        shapes.append(out)
        pen_x += ink + track
    return shapes, pen_x - track - (x or 0)


def fill_nonzero(d, contours, k):
    """The font's outlines overlap themselves, so this fills them by winding number, one scanline at a time."""
    edges = []
    for c in contours:
        pts = [(x * k, y * k) for x, y in c]
        edges += [(a, b) for a, b in zip(pts, pts[1:] + pts[:1]) if a[1] != b[1]]
    for row in range(int(min(min(a[1], b[1]) for a, b in edges)), int(max(max(a[1], b[1]) for a, b in edges)) + 1):
        y = row + 0.5
        hits = sorted((a[0] + (y - a[1]) / (b[1] - a[1]) * (b[0] - a[0]), 1 if b[1] > a[1] else -1)
                      for a, b in edges if min(a[1], b[1]) <= y < max(a[1], b[1]))
        wind, start = 0, 0
        for x, step in hits:
            if wind == 0:
                start = x
            wind += step
            if wind == 0 and round(x) > round(start):
                d.rectangle([round(start), row, round(x) - 1, row], fill=255)


def build(path):
    big, bs, bw = face(path, BIG_STEM, BIG_CAP)
    small, ss, sw = face(path, SMALL_STEM, SMALL_CAP)
    block = SMALL_CAP + GAP + BIG_CAP
    top = (H - block) // 2
    base_small, base_big = top + SMALL_CAP, top + block
    _, big_w = word(big, bs, "CORE", BIG_TRACK, base_big)
    _, small_w = word(small, ss, "MEDIA", SMALL_TRACK, base_small)
    shapes, _ = word(big, bs, "CORE", BIG_TRACK, base_big, (W - big_w) // 2)
    letters, _ = word(small, ss, "MEDIA", SMALL_TRACK, base_small, (W - small_w) // 2)
    print("weights %.0f / %.0f, CORE %d px wide, MEDIA %d px wide, top %d" % (bw, sw, big_w, small_w, top))
    ring = next(s[1] for s in shapes if isinstance(s, tuple))
    r = BIG_CAP / 2 + OVERSHOOT
    cx, cy = ring + r, base_big - BIG_CAP / 2
    return [s for s in shapes + letters if not isinstance(s, tuple)], (cx, cy, r)


def rasterize(outlines, ring):
    k = SCALE * SS
    ink = Image.new("L", (W * k, H * k), 0)
    d = ImageDraw.Draw(ink)
    for o in outlines:
        fill_nonzero(d, o.contours, k)
    cx, cy, r = ring
    circle = lambda rad, fill: d.ellipse([(cx - rad) * k, (cy - rad) * k, (cx + rad) * k, (cy + rad) * k], fill=fill)
    circle(r, 255)
    circle(r - BIG_STEM, 0)
    core = Image.new("L", ink.size, 0)
    ImageDraw.Draw(core).ellipse(
        [(cx - BIG_STEM * CORE_RATIO) * k, (cy - BIG_STEM * CORE_RATIO) * k,
         (cx + BIG_STEM * CORE_RATIO) * k, (cy + BIG_STEM * CORE_RATIO) * k], fill=255)
    size = (W * SCALE, H * SCALE)
    ink, core = ink.resize(size, Image.BOX), core.resize(size, Image.BOX)
    im = Image.new("RGB", size, BG)
    im.paste(Image.new("RGB", size, INK), mask=ink)
    im.paste(Image.new("RGB", size, CORE), mask=core)
    return im


def svgs(outlines, ring):
    cx, cy, r = ring
    letters = " ".join("".join(o.svg) for o in outlines)
    stroke_r = r - BIG_STEM / 2
    with open(os.path.join(HERE, "wordmark.svg"), "w") as f:
        f.write('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d"><title>Media Core</title>\n'
                '<path fill="%s" d="%s"/>\n'
                '<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="%d"/>\n'
                '<circle cx="%g" cy="%g" r="%g" fill="%s"/>\n</svg>\n'
                % (W, H, INK, letters, cx, cy, stroke_r, INK, BIG_STEM, cx, cy, BIG_STEM * CORE_RATIO, CORE))
    # the O alone: same proportions as in the wordmark, on a 108 canvas like the app icon's
    k = 60 / (2 * r)
    with open(os.path.join(HERE, "mark.svg"), "w") as f:
        f.write('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 108 108"><title>Media Core</title>\n'
                '<circle cx="54" cy="54" r="%g" fill="none" stroke="%s" stroke-width="%g"/>\n'
                '<circle cx="54" cy="54" r="%g" fill="%s"/>\n</svg>\n'
                % ((r - BIG_STEM / 2) * k, INK, BIG_STEM * k, BIG_STEM * CORE_RATIO * k, CORE))
    print("mark: ring r %.2f stroke %.2f core r %.2f" % ((r - BIG_STEM / 2) * k, BIG_STEM * k, BIG_STEM * CORE_RATIO * k))


if __name__ == "__main__":
    outlines, ring = build(sys.argv[1])
    svgs(outlines, ring)
    rasterize(outlines, ring).save(BANNER, optimize=True)
