#!/usr/bin/env python3
# Generates icon.ico (the tray icon, also embedded as both .exe icons) from
# scratch with Pillow.
# Run: python3 assets/gen_icon.py
from PIL import Image, ImageDraw, ImageFilter

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
FADER_LEVELS = (0.62, 0.28, 0.48)


def draw_mixer(draw, cx, cy, scale):
    # Three vertical fader rails, each with a round knob crossing it -
    # the mixing-console glyph, pared down to just that (an earlier
    # version also drew a frame around it and used 4 thinner rails; both
    # turned out to blur into a fuzzy mass at real 16px tray size,
    # leaving only the knobs as distinguishable dots). Fewer, fatter,
    # more widely spaced shapes read as a distinct icon at a glance
    # instead of a smudge indistinguishable from any other app's.
    m = scale

    rail_w = 30 * m
    rail_half_len = 200 * m
    knob_r = 62 * m
    spacing = 168 * m

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


def build(size):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    scale = size / SIZE
    cx, cy = size / 2, size / 2

    draw_mixer(draw, cx=cx, cy=cy, scale=scale)

    return add_outline(img, radius_px=size * 0.02)


def main():
    # assets/icons is the single source of truth: it's go:embed'ed into the
    # switcher binary for the tray icon, and also fed to the Windows
    # resource compiler (rsrc) in CI to set both .exe icons.
    icon = build(SIZE)
    icon.save("assets/icons/icon.ico", sizes=[(s, s) for s in SIZES])
    icon.save("assets/icon_preview.png")
    print("Wrote assets/icons/icon.ico (+ PNG preview)")


if __name__ == "__main__":
    main()
