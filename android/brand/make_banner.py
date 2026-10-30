import sys
from PIL import Image, ImageDraw, ImageFont
font_path, out = sys.argv[1], sys.argv[2]
S = 4; W, H = 640*S, 360*S
BG_EDGE = (8, 10, 18); BG_MID = (15, 19, 36)
bg = Image.new('RGB', (160, 90))
for y in range(90):
    for x in range(160):
        dx, dy = (x-80)/80, (y-45)/45
        t = max(0, 1-(dx*dx+dy*dy)*0.9)
        bg.putpixel((x, y), tuple(int(a+(b-a)*t) for a, b in zip(BG_EDGE, BG_MID)))
im = bg.resize((W, H), Image.BICUBIC)
d = ImageDraw.Draw(im)
INK = (244, 245, 248)
def font(size, w):
    f = ImageFont.truetype(font_path, size); f.set_variation_by_axes([w]); return f
def ink(f, c):  # left edge and width of the letter's actual ink, from the pen position
    b = f.getmask(c).getbbox(); return f.getbbox(c)[0] + b[0], b[2]-b[0]
big = font(96*S, 500); small = font(32*S, 500)
cap_h = big.getbbox('E')[3]-big.getbbox('E')[1]
st = ink(big, 'I')[1]
tr = 25*S
ring = cap_h + 3*S
wC, wR, wE = ink(big,'C')[1], ink(big,'R')[1], ink(big,'E')[1]
total = wC + tr + ring + tr + wR + tr + wE
x = (W-total)//2
s_tr = 10*S
s_w = [ink(small,c)[1] for c in 'MEDIA']
s_total = sum(s_w)+s_tr*4
s_cap = small.getbbox('E')[3]-small.getbbox('E')[1]
gap = 25*S
block = s_cap + gap + cap_h
top = (H-block)//2
base_s = top + s_cap; base_b = top + s_cap + gap + cap_h
sx = (W-s_total)//2
for c, w in zip('MEDIA', s_w):
    d.text((sx-ink(small,c)[0], base_s), c, font=small, fill=INK, anchor='ls'); sx += w + s_tr
def letter(c):
    global x
    l, w = ink(big,c); d.text((x-l, base_b), c, font=big, fill=INK, anchor='ls'); x += w + tr
letter('C')
cy = base_b - cap_h/2; r = ring/2; cx = x + r
d.ellipse((cx-r, cy-r, cx+r, cy+r), fill=INK)
inner = r - st
d.ellipse((cx-inner, cy-inner, cx+inner, cy+inner), fill=im.getpixel((int(cx), int(cy-r-6*S))))
core = st*1.05
d.ellipse((cx-core, cy-core, cx+core, cy+core), fill=INK)
x += ring + tr
letter('R'); letter('E')
im.resize((640, 360), Image.LANCZOS).save(out, optimize=True)
print('stem', st/S, 'ring r', r/S, 'inner', inner/S, 'core', core/S, 'width', total/S)
