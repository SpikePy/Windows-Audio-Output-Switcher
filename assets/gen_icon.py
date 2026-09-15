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
# it is light or dark. The composition itself is also kept to few, bold
# shapes rather than thin linework: fine detail reads fine at 512px but
# collapses into a smudge once Windows draws it at 16px in the tray.
RAIL = (255, 255, 255)
HANDLE = (46, 230, 168)  # flat mint accent for each fader's handle
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


# Each fader's knob sits at a different height along its rail - purely
# for visual variety (a mixing console reads as "audio" partly because
# its faders are never all level), not any actual meaning.
FADER_LEVELS = (0.66, 0.30, 0.55, 0.40)


def draw_mixer(draw, cx, cy, scale):
    # A rounded-square frame around a row of vertical fader rails, each
    # with a round knob crossing it - the classic mixing-console glyph.
    # Bold strokes and knobs (not hairlines) so it stays readable shrunk
    # to a 16px tray icon.
    m = scale

    frame_half = 216 * m
    frame_radius = 70 * m
    frame_width = 26 * m
    draw.rounded_rectangle(
        [cx - frame_half, cy - frame_half, cx + frame_half, cy + frame_half],
        radius=frame_radius,
        outline=RAIL,
        width=round(frame_width),
    )

    rail_w = 16 * m
    rail_half_len = 140 * m
    knob_r = 34 * m
    spacing = 108 * m

    top = cy - rail_half_len
    bottom = cy + rail_half_len

    for i, level in enumerate(FADER_LEVELS):
        x = cx + (i - (len(FADER_LEVELS) - 1) / 2) * spacing
        draw.rounded_rectangle(
            [x - rail_w / 2, top, x + rail_w / 2, bottom],
            radius=rail_w / 2,
            fill=RAIL,
        )
        knob_y = top + rail_half_len * 2 * level
        draw.ellipse(
            [x - knob_r, knob_y - knob_r, x + knob_r, knob_y + knob_r],
            fill=HANDLE,
        )


def build(size, muted):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    scale = size / SIZE
    cx, cy = size / 2, size / 2

    draw_mixer(draw, cx=cx, cy=cy, scale=scale)

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
