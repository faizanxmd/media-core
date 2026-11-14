#!/usr/bin/env python3
"""Draws the Media Core logo from Montserrat's outlines.

Usage: PYTHONPATH=<fonttools> make_logo.py <Montserrat[wght].ttf> [banner.png]
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
BG, INK, CORE = "#090B0E", "#F4F5F8", "#1D39F3"
LAMP = [(0, "#7FA2FF"), (0.55, "#3A5CF8"), (1, CORE)]  # the core is lit: bright centre, cobalt edge
HALO = 0.7         # its light across the O's counter: this opacity at the centre, none at the ring

BIG_CAP, BIG_STEM = 46, 9                     # CORE, in grid pixels
BIG_TRACK = {"CO": 12, "OR": 14, "RE": 16}   # by eye: round pairs sit closer than straight ones
SMALL_CAP, SMALL_STEM, SMALL_TRACK = 15, 3, 7 # MEDIA
GAP = 18
CORE_RATIO = 1.0    # core radius, in stems: leaves the counter room to glow

HERE = os.path.dirname(os.path.abspath(__file__))
BANNER = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "../app/src/main/res/drawable-xhdpi/banner.png")


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


def o_shape(f, scale):
    """Montserrat's O at this weight: its height, and its stroke at the sides and at the top and bottom."""
    out = Outline(f.getGlyphSet(), scale, 0, 0)
    f.getGlyphSet()[f.getBestCmap()[ord("O")]].draw(out)
    (ox0, oy0, ox1, oy1), (ix0, iy0, ix1, iy1) = sorted(
        ((min(x for x, _ in c), min(y for _, y in c), max(x for x, _ in c), max(y for _, y in c)) for c in out.contours),
        key=lambda b: b[0])
    r = (oy1 - oy0) / 2
    side = ((ix0 - ox0) + (ox1 - ix1)) / 2
    top = ((iy0 - oy0) + (oy1 - iy1)) / 2
    return r, side, top, (oy0 + oy1) / 2


def word(f, scale, text, track, baseline, x=None):
    """Lays the letters out with their ink on whole pixels. x None measures only."""
    pen_x = x or 0
    shapes = []
    gap = lambda i: track.get(text[i:i + 2], 0) if isinstance(track, dict) else track
    for i, c in enumerate(text):
        lo, hi = extents(f, c)
        if c == "O":
            shapes.append(("O", pen_x))
            pen_x += 2 * o_shape(f, scale)[0] + gap(i)
            continue
        ink = round((hi - lo) * scale)  # stretch by under a pixel so the right edge lands on the grid too
        out = Outline(f.getGlyphSet(), scale, pen_x - lo * ink / (hi - lo), baseline, ink / (hi - lo))
        f.getGlyphSet()[f.getBestCmap()[ord(c)]].draw(out)
        shapes.append(out)
        pen_x += ink + gap(i)
    return shapes, pen_x - gap(len(text) - 1) - (x or 0)


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
    r, side, top_, mid = o_shape(big, bs)
    cx, cy = ring + r, base_big + mid
    print("O: r %.2f, stroke %.2f at the sides, %.2f at top and bottom" % (r, side, top_))
    return [s for s in shapes + letters if not isinstance(s, tuple)], (cx, cy, r, r - side, r - top_)


def hex_rgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


def lamp_at(t):
    """The core's colour at t (0 centre, 1 edge), along LAMP."""
    for (t0, c0), (t1, c1) in zip(LAMP, LAMP[1:]):
        if t <= t1:
            u = (t - t0) / (t1 - t0)
            return tuple(round(a + (b - a) * u) for a, b in zip(hex_rgb(c0), hex_rgb(c1)))
    return hex_rgb(LAMP[-1][1])


def radial(size, cx, cy, rad, colour, alpha):
    """An RGBA picture of a radial fill: colour(t) and alpha(t) for t = distance / rad, nothing past rad."""
    im = Image.new("RGBA", size, (0, 0, 0, 0))
    px = im.load()
    for y in range(max(0, int(cy - rad)), min(size[1], int(cy + rad) + 2)):
        for x in range(max(0, int(cx - rad)), min(size[0], int(cx + rad) + 2)):
            t = ((x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2) ** 0.5 / rad
            if t < 1:
                px[x, y] = colour(t) + (round(255 * alpha(t)),)
    return im


def rasterize(outlines, ring):
    k = SCALE * SS
    ink = Image.new("L", (W * k, H * k), 0)
    d = ImageDraw.Draw(ink)
    for o in outlines:
        fill_nonzero(d, o.contours, k)
    cx, cy, r, rx, ry = ring
    cr = BIG_STEM * CORE_RATIO
    d.ellipse([(cx - r) * k, (cy - r) * k, (cx + r) * k, (cy + r) * k], fill=255)
    d.ellipse([(cx - rx) * k, (cy - ry) * k, (cx + rx) * k, (cy + ry) * k], fill=0)
    core = Image.new("L", ink.size, 0)
    ImageDraw.Draw(core).ellipse([(cx - cr) * k, (cy - cr) * k, (cx + cr) * k, (cy + cr) * k], fill=255)
    size = (W * SCALE, H * SCALE)
    ink, core = ink.resize(size, Image.BOX), core.resize(size, Image.BOX)
    s = SCALE
    im = Image.new("RGBA", size, BG)
    im.alpha_composite(radial(size, cx * s, cy * s, ry * s, lambda t: hex_rgb(CORE), lambda t: HALO * (1 - t)))
    im.paste(Image.new("RGBA", size, INK), mask=ink)
    lit = radial(size, cx * s, cy * s, (cr + 1) * s, lambda t: lamp_at(min(1, t * (cr + 1) / cr)), lambda t: 1)
    im.paste(lit, mask=core)
    return im.convert("RGB")


def o_svg(cx, cy, r, rx, ry, cr):
    """The O: ring with the font's stroke, the core's light across its counter, and the lit core."""
    g = lambda v: "%g" % round(v, 3)
    stops = "".join('<stop offset="%g" stop-color="%s"/>' % (t, c) for t, c in LAMP)
    return ('<defs><radialGradient id="halo"><stop offset="0" stop-color="%s" stop-opacity="%g"/>'
            '<stop offset="1" stop-color="%s" stop-opacity="0"/></radialGradient>'
            '<radialGradient id="lamp">%s</radialGradient></defs>\n'
            '<circle cx="%s" cy="%s" r="%s" fill="url(#halo)"/>\n'
            '<path fill="%s" fill-rule="evenodd" d="M%s,%sa%s,%s 0 1,0 %s,0a%s,%s 0 1,0 -%s,0ZM%s,%sa%s,%s 0 1,0 %s,0a%s,%s 0 1,0 -%s,0Z"/>\n'
            '<circle cx="%s" cy="%s" r="%s" fill="url(#lamp)"/>\n'
            % (CORE, HALO, CORE, stops, g(cx), g(cy), g(ry), INK,
               g(cx - r), g(cy), g(r), g(r), g(2 * r), g(r), g(r), g(2 * r),
               g(cx - rx), g(cy), g(rx), g(ry), g(2 * rx), g(rx), g(ry), g(2 * rx),
               g(cx), g(cy), g(cr)))


def svgs(outlines, ring):
    cx, cy, r, rx, ry = ring
    cr = BIG_STEM * CORE_RATIO
    letters = " ".join("".join(o.svg) for o in outlines)
    with open(os.path.join(HERE, "wordmark.svg"), "w") as f:
        f.write('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d"><title>Media Core</title>\n'
                '<path fill="%s" d="%s"/>\n%s</svg>\n' % (W, H, INK, letters, o_svg(cx, cy, r, rx, ry, cr)))
    # the O alone: same proportions as in the wordmark, on a 108 canvas like the app icon's
    k = 60 / (2 * r)
    with open(os.path.join(HERE, "mark.svg"), "w") as f:
        f.write('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 108 108"><title>Media Core</title>\n%s</svg>\n'
                % o_svg(54, 54, 30, rx * k, ry * k, cr * k))
    print("mark: ring r 30, inner %.2f x %.2f, core r %.2f" % (rx * k, ry * k, cr * k))


if __name__ == "__main__":
    outlines, ring = build(sys.argv[1])
    svgs(outlines, ring)
    rasterize(outlines, ring).save(BANNER, optimize=True)
