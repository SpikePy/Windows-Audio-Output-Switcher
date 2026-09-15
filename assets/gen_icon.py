#!/usr/bin/env python3
# Generates icon.ico (colored, "enabled" state + exe icon) and
# icon_disabled.ico (muted, "disabled" tray state) from scratch with Pillow.
# Run: python3 assets/gen_icon.py
from PIL import Image, ImageDraw
import math

SIZE = 512
SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]

BG_TOP = (79, 108, 247)      # #4F6CF7
BG_BOTTOM = (137, 87, 230)   # #8957E6
SPEAKER = (255, 255, 255)
SWITCH_COLOR = (86, 234, 189)   # minty accent for the "switch" motif
SWITCH_SHADOW = (54, 176, 143)


def rounded_mask(size, radius):
    mask = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(mask)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=255)
    return mask


def gradient_background(size):
    base = Image.new("RGB", (size, size))
    px = base.load()
    for y in range(size):
        t = y / (size - 1)
        r = round(BG_TOP[0] + (BG_BOTTOM[0] - BG_TOP[0]) * t)
        g = round(BG_TOP[1] + (BG_BOTTOM[1] - BG_TOP[1]) * t)
        b = round(BG_TOP[2] + (BG_BOTTOM[2] - BG_TOP[2]) * t)
        for x in range(size):
            px[x, y] = (r, g, b)
    return base


def draw_speaker(draw, cx, cy, scale):
    # Classic "speaker" glyph: a small body plus an expanding horn, in one polygon.
    body_w = 70 * scale
    body_h = 130 * scale
    horn_w = 90 * scale
    horn_h = 230 * scale
    x0 = cx - (body_w + horn_w) / 2
    points = [
        (x0, cy - body_h / 2),
        (x0 + body_w, cy - body_h / 2),
        (x0 + body_w + horn_w, cy - horn_h / 2),
        (x0 + body_w + horn_w, cy + horn_h / 2),
        (x0 + body_w, cy + body_h / 2),
        (x0, cy + body_h / 2),
    ]
    draw.polygon(points, fill=SPEAKER)


def draw_switch_arrows(draw, cx, cy, radius, width, color, shadow):
    bbox = [cx - radius, cy - radius, cx + radius, cy + radius]
    # Two opposing arcs forming a circular "swap/cycle" motif.
    draw.arc(bbox, start=-160, end=40, fill=shadow, width=width + 6)
    draw.arc(bbox, start=20, end=220, fill=shadow, width=width + 6)
    draw.arc(bbox, start=-160, end=40, fill=color, width=width)
    draw.arc(bbox, start=20, end=220, fill=color, width=width)

    def arrowhead(angle_deg, color):
        a = math.radians(angle_deg)
        tip = (cx + radius * math.cos(a), cy + radius * math.sin(a))
        back = (
            cx + (radius - width * 1.8) * math.cos(a),
            cy + (radius - width * 1.8) * math.sin(a),
        )
        perp = a + math.pi / 2
        spread = width * 1.3
        p1 = (back[0] + spread * math.cos(perp), back[1] + spread * math.sin(perp))
        p2 = (back[0] - spread * math.cos(perp), back[1] - spread * math.sin(perp))
        tip_ext = (
            cx + (radius + width * 0.9) * math.cos(a),
            cy + (radius + width * 0.9) * math.sin(a),
        )
        draw.polygon([tip_ext, p1, p2], fill=color)

    arrowhead(40, color)
    arrowhead(220, color)


def build(size, muted):
    bg = gradient_background(size)
    mask = rounded_mask(size, radius=int(size * 0.22))
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    img.paste(bg, (0, 0), mask)

    draw = ImageDraw.Draw(img)
    scale = size / SIZE
    draw_speaker(draw, cx=size * 0.34, cy=size * 0.52, scale=scale)
    draw_switch_arrows(
        draw,
        cx=size * 0.665,
        cy=size * 0.52,
        radius=size * 0.175,
        width=max(2, round(size * 0.045)),
        color=SWITCH_COLOR,
        shadow=SWITCH_SHADOW,
    )

    if muted:
        gray = img.convert("LA").convert("RGBA")
        img = Image.blend(img, gray, 0.85)
        alpha = img.split()[3].point(lambda a: int(a * 0.55))
        img.putalpha(alpha)

    return img


def main():
    # assets/icons is the single source of truth: it's go:embed'ed into the
    # switcher binary for the tray icon, and also fed to the Windows
    # resource compiler (rsrc) in CI to set the .exe icon.
    icon = build(SIZE, muted=False)
    icon.save("assets/icons/icon.ico", sizes=[(s, s) for s in SIZES])

    disabled = build(SIZE, muted=True)
    disabled.save("assets/icons/icon_disabled.ico", sizes=[(s, s) for s in SIZES])

    icon.save("assets/icon_preview.png")
    disabled.save("assets/icon_disabled_preview.png")
    print("Wrote assets/icons/icon.ico, assets/icons/icon_disabled.ico (+ PNG previews)")


if __name__ == "__main__":
    main()
