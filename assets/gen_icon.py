#!/usr/bin/env python3
# Generates icon.ico (colored, "enabled" state + exe icon) and
# icon_disabled.ico (muted, "disabled" tray state) from scratch with Pillow.
# Run: python3 assets/gen_icon.py
from PIL import Image, ImageDraw
import math

SIZE = 512
SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]

# Flat design: solid colors only, no gradients or drop shadows.
BG_COLOR = (88, 101, 242)      # flat indigo
SPEAKER = (255, 255, 255)
SWITCH_COLOR = (46, 230, 168)  # flat mint accent for the "switch" motif


def rounded_mask(size, radius):
    mask = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(mask)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=255)
    return mask


def flat_background(size):
    return Image.new("RGB", (size, size), BG_COLOR)


def draw_speaker(draw, cx, cy, scale, size_mult=1.0):
    # Classic "speaker" glyph: a small body plus an expanding horn, in one polygon.
    m = scale * size_mult
    body_w = 70 * m
    body_h = 130 * m
    horn_w = 90 * m
    horn_h = 230 * m
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


def draw_switch_arrows(draw, cx, cy, radius, width, color):
    bbox = [cx - radius, cy - radius, cx + radius, cy + radius]
    # Two opposing arcs forming a circular "swap/cycle" motif - a single
    # flat stroke each, no shadow/bevel layer underneath.
    draw.arc(bbox, start=-160, end=40, fill=color, width=width)
    draw.arc(bbox, start=20, end=220, fill=color, width=width)

    def arrowhead(angle_deg, color):
        # A chevron sitting ON the ring, pointing tangentially (i.e.
        # along the circle's curve, in the arc's sweep direction) rather
        # than radially outward - the classic "refresh/cycle" look.
        a = math.radians(angle_deg)
        radial = (math.cos(a), math.sin(a))
        tangent = (-math.sin(a), math.cos(a))  # direction of increasing angle

        center = (cx + radius * radial[0], cy + radius * radial[1])
        head_len = width * 2.6
        head_w = width * 1.5

        tip = (
            center[0] + tangent[0] * head_len * 0.55,
            center[1] + tangent[1] * head_len * 0.55,
        )
        base = (
            center[0] - tangent[0] * head_len * 0.45,
            center[1] - tangent[1] * head_len * 0.45,
        )
        p1 = (base[0] + radial[0] * head_w, base[1] + radial[1] * head_w)
        p2 = (base[0] - radial[0] * head_w, base[1] - radial[1] * head_w)
        draw.polygon([tip, p1, p2], fill=color)

    arrowhead(40, color)
    arrowhead(220, color)


def build(size, muted):
    bg = flat_background(size)
    mask = rounded_mask(size, radius=int(size * 0.22))
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    img.paste(bg, (0, 0), mask)

    draw = ImageDraw.Draw(img)
    scale = size / SIZE
    cx, cy = size / 2, size / 2

    # A speaker sitting inside a circular loop of arrows: the loop reads as
    # "switch/cycle", the speaker as "audio output".
    draw_switch_arrows(
        draw,
        cx=cx,
        cy=cy,
        radius=size * 0.30,
        width=max(2, round(size * 0.048)),
        color=SWITCH_COLOR,
    )
    draw_speaker(draw, cx=cx, cy=cy, scale=scale, size_mult=0.78)

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
