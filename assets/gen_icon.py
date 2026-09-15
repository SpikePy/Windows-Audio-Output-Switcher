#!/usr/bin/env python3
# Generates icon.ico (colored, "enabled" state + exe icon) and
# icon_disabled.ico (muted, "disabled" tray state) from scratch with Pillow.
# Run: python3 assets/gen_icon.py
from PIL import Image, ImageDraw, ImageFilter
import math

SIZE = 512
SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]

# Flat design: solid colors only, no gradients or drop shadows. There's no
# background plate - the glyph sits on a transparent canvas so the tray's
# own background shows through - so the whole silhouette gets a dark
# outline (see add_outline below) to stay legible whether the tray behind
# it is light or dark. The composition itself is also kept to two bold
# shapes (a speaker plus a small corner badge) rather than thin linework:
# a full ring-and-arrowheads motif reads fine at 512px but collapses into
# a smudge once Windows draws it at 16px in the tray.
SPEAKER = (255, 255, 255)
SWITCH_COLOR = (46, 230, 168)  # flat mint accent for the "switch" badge
OUTLINE_COLOR = (18, 18, 26)


def add_outline(img, radius_px):
    """Returns img composited over a dark silhouette outline traced
    radius_px beyond its own alpha - a "sticker" edge that keeps the
    glyph readable on any background color, since a transparent icon
    can't rely on a background plate for contrast."""
    alpha = img.split()[3]
    dilated = alpha.filter(ImageFilter.GaussianBlur(radius_px)).point(
        lambda a: 255 if a > 12 else 0
    )
    outline = Image.new("RGBA", img.size, OUTLINE_COLOR + (255,))
    outline.putalpha(dilated)
    return Image.alpha_composite(outline, img)


def draw_speaker(draw, cx, cy, scale):
    # Classic "speaker" glyph: a small body plus an expanding horn, in one
    # polygon. This is the dominant shape - bold and solid so it stays
    # readable even shrunk to a 16px tray icon.
    m = scale
    body_w = 92 * m
    body_h = 168 * m
    horn_w = 118 * m
    horn_h = 296 * m
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


def draw_switch_badge(draw, cx, cy, r):
    # A small solid disc badge (not a thin ring around the whole icon) so
    # it stays a single clean blob of color at tiny sizes, with a pair of
    # small cycle arrows inside it that only need to read at larger
    # sizes - a common "base glyph + corner badge" pattern.
    draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=SWITCH_COLOR)

    ring_r = r * 0.56
    width = max(1, round(r * 0.28))
    bbox = [cx - ring_r, cy - ring_r, cx + ring_r, cy + ring_r]
    draw.arc(bbox, start=-160, end=40, fill=SPEAKER, width=width)
    draw.arc(bbox, start=20, end=220, fill=SPEAKER, width=width)

    def arrowhead(angle_deg):
        a = math.radians(angle_deg)
        radial = (math.cos(a), math.sin(a))
        tangent = (-math.sin(a), math.cos(a))
        center = (cx + ring_r * radial[0], cy + ring_r * radial[1])
        head_len = width * 2.4
        head_w = width * 1.4
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
        draw.polygon([tip, p1, p2], fill=SPEAKER)

    arrowhead(40)
    arrowhead(220)


def build(size, muted):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    scale = size / SIZE
    cx, cy = size / 2, size / 2

    draw_speaker(draw, cx=cx - size * 0.03, cy=cy, scale=scale * 1.0)
    draw_switch_badge(
        draw,
        cx=cx + size * 0.30,
        cy=cy + size * 0.30,
        r=size * 0.24,
    )

    img = add_outline(img, radius_px=size * 0.02)

    if muted:
        gray = img.convert("LA").convert("RGBA")
        img = Image.blend(img, gray, 0.85)
        alpha = img.split()[3].point(lambda a: int(a * 0.7))
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
