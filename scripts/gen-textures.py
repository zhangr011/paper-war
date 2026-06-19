#!/usr/bin/env python3
"""
Paper War — procedural UI texture generator.

Generates the 6 small PNG textures used by client/style.css to give the
DOM chrome a paper / parchment / wood / gold-leaf look.

Outputs (client/assets/textures/):
  - parchment-tile.png      256x256 tileable paper grain (warm)
  - parchment-warm.png      256x256 tileable slightly darker tan
  - wood-frame-9slice.png   64x64  9-slice panel border (golden-brown bevel)
  - navy-header-tile.png    128x32 dark navy header strip (tileable horizontally)
  - gold-cta-tile.png       128x32 vivid gold CTA strip (tileable horizontally)
  - ink-border.png          64x64  1px brown ink-stroke (tileable)

All assets are deterministic given the fixed SEED below, so re-running the
script produces byte-identical PNGs.

Usage:
    python3 scripts/gen-textures.py            # writes to client/assets/textures/
    python3 scripts/gen-textures.py --out /tmp # writes to /tmp

Requires Pillow (`pip install Pillow`).
"""
import argparse
import math
import os
import random
import sys

try:
    from PIL import Image, ImageDraw, ImageFilter
except ImportError:
    sys.stderr.write("Pillow not installed. Run: pip install Pillow\n")
    sys.exit(1)

# Deterministic output — re-running produces byte-identical files.
SEED = 20260619


# ---------- helpers ---------------------------------------------------------

def _noise(size, rng, low, high, channels=3):
    """Return a list of ints in [low, high] (NOT clamped to 0..255 — caller
    handles wraparound when adding to a base value)."""
    n = size[0] * size[1] * channels
    return [rng.randint(low, high) for _ in range(n)]


def _tileable_value_noise(w, h, rng, scale=4, amp=16, base=128, channels=3):
    """Generate tileable value-noise by bilinear-interpolating a low-res
    grid that wraps in both axes. Returns HxWxC uint8 bytes."""
    import array
    gw, gh = max(2, w // scale), max(2, h // scale)
    # Random grid points (wrap-around by indexing with modulo)
    grid = [[tuple(rng.randint(0, 255) for _ in range(channels))
             for _ in range(gw)] for _ in range(gh)]
    buf = array.array("B")
    for y in range(h):
        gy = (y / h) * gh
        iy = int(gy) % gh
        fy = gy - int(gy)
        # Smoothstep for less linear-looking transitions
        fy = fy * fy * (3 - 2 * fy)
        row0 = grid[iy]
        row1 = grid[(iy + 1) % gh]
        for x in range(w):
            gx = (x / w) * gw
            ix = int(gx) % gw
            fx = gx - int(gx)
            fx = fx * fx * (3 - 2 * fx)
            c00 = row0[ix]
            c10 = row0[(ix + 1) % gw]
            c01 = row1[ix]
            c11 = row1[(ix + 1) % gw]
            for c in range(channels):
                top = c00[c] * (1 - fx) + c10[c] * fx
                bot = c01[c] * (1 - fx) + c11[c] * fx
                v = top * (1 - fy) + bot * fy
                # Blend toward base by amp/255, then add high-freq noise later.
                out = int(round(base + (v - 128) * (amp / 255.0) * 2))
                buf.append(max(0, min(255, out)))
    return buf


def make_parchment(size, rng, base_rgb, warmth=0, fiber_strength=14,
                   stain_strength=22, stains=8):
    """Tileable parchment texture.

    base_rgb: (r,g,b) average color
    warmth:   +/- offset on R and B channels (positive = warmer/redder)
    """
    w, h = size
    import array
    # Base value-noise field blended toward base_rgb
    field = _tileable_value_noise(w, h, rng, scale=6, amp=fiber_strength * 2,
                                  base=128, channels=3)
    # High-frequency speckle
    speckle = _noise(size, rng, -8, 8, channels=3)

    # Random ink/coffee stains — tileable (place near edges too, wrap)
    stain_img = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    sd = ImageDraw.Draw(stain_img)
    br, bg, bb = base_rgb
    for _ in range(stains):
        cx = rng.randint(0, w - 1)
        cy = rng.randint(0, h - 1)
        rx = rng.randint(8, 22)
        ry = rng.randint(8, 22)
        alpha = rng.randint(int(stain_strength * 0.6), stain_strength)
        # Slight brownish stain tint
        sr = max(0, br - 60)
        sg = max(0, bg - 70)
        sb = max(0, bb - 80)
        for ox, oy in ((0, 0), (1, 0), (0, 1), (1, 1)):
            # Stamp on 4-tile corners so wrap is seamless
            px = (cx + ox * w) % w
            py = (cy + oy * h) % h
            sd.ellipse([px - rx, py - ry, px + rx, py + ry],
                       fill=(sr, sg, sb, alpha))
    stain_img = stain_img.filter(ImageFilter.GaussianBlur(radius=4))

    out = Image.new("RGB", (w, h))
    px = out.load()
    i = 0
    s_i = 0
    for y in range(h):
        for x in range(w):
            # base + value-noise delta + speckle
            fr = br + (field[i] - 128) // 4 + warmth + speckle[s_i]
            fg = bg + (field[i + 1] - 128) // 4 + speckle[s_i + 1]
            fb = bb + (field[i + 2] - 128) // 4 - warmth + speckle[s_i + 2]
            i += 3
            s_i += 3
            px[x, y] = (max(0, min(255, fr)),
                        max(0, min(255, fg)),
                        max(0, min(255, fb)))
    # Composite stains
    out = Image.alpha_composite(out.convert("RGBA"), stain_img).convert("RGB")
    return out


def make_wood_frame_9slice(size=(64, 64), border=14):
    """Golden-brown beveled 9-slice border. Center is transparent so the
    panel's underlying parchment shows through; only the frame ring is opaque.
    border = pixel width of the frame ring on each side."""
    w, h = size
    rng = random.Random(SEED + 7)
    img = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    # Generate a horizontal wood-grain strip then reshape into a ring
    strip_w = 2 * (w + h)
    strip_h = border
    strip = Image.new("RGB", (strip_w, strip_h))
    sp = strip.load()
    # Base golden-brown gradient + horizontal grain
    for y in range(strip_h):
        # Bevel: top half lighter (highlight), bottom half darker (shadow)
        t = y / max(1, strip_h - 1)  # 0..1
        if t < 0.45:
            # Highlight band
            shade = 0.78 + 0.22 * (1 - abs(t - 0.20) / 0.25)
        elif t < 0.75:
            # Mid
            shade = 0.78
        else:
            # Inner shadow
            shade = 0.55 - 0.18 * ((t - 0.75) / 0.25)
        shade = max(0.35, min(1.05, shade))
        for x in range(strip_w):
            # Long-wavelength grain
            grain = math.sin(x * 0.08 + y * 0.7) * 6 + math.sin(x * 0.021) * 10
            noise = rng.randint(-6, 6)
            r = int((205 + grain + noise) * shade)
            g = int((160 + grain * 0.8 + noise) * shade)
            b = int((95 + grain * 0.5 + noise) * shade)
            sp[x, y] = (max(0, min(255, r)),
                        max(0, min(255, g)),
                        max(0, min(255, b)))
    # Now wrap the strip into the frame ring
    # Top edge
    img.paste(strip.crop((0, 0, w, border)), (0, 0))
    # Bottom edge (use a shifted segment so grain flows)
    img.paste(strip.crop((w, 0, 2 * w, border)), (0, h - border))
    # Left edge — rotate
    left = strip.crop((2 * w, 0, 2 * w + h, border)).rotate(90, expand=True)
    img.paste(left, (0, border))
    # Right edge
    right = strip.crop((2 * w + h, 0, 2 * w + 2 * h, border)).rotate(90, expand=True)
    img.paste(right, (w - border, border))
    # Corner overlaps already filled by top/bottom pastes; ensure corners are opaque
    return img


def make_strip(size, rng, base_rgb, amp=10, fibers=True, stain_strength=12,
               stains=4):
    """Tileable horizontal strip texture (for header / CTA bars)."""
    w, h = size
    import array
    field = _tileable_value_noise(w, h, rng, scale=5, amp=amp * 2, base=128, channels=3)
    speckle = _noise(size, rng, -4, 4, channels=3)
    out = Image.new("RGB", (w, h))
    px = out.load()
    br, bg, bb = base_rgb
    i = 0
    s_i = 0
    for y in range(h):
        for x in range(w):
            # Add a vertical sheen: slightly brighter in middle, darker top/bottom
            sheen = int(8 * math.sin((y / max(1, h - 1)) * math.pi))
            fr = br + (field[i] - 128) // 6 + speckle[s_i] + sheen
            fg = bg + (field[i + 1] - 128) // 6 + speckle[s_i + 1] + sheen
            fb = bb + (field[i + 2] - 128) // 6 + speckle[s_i + 2] + sheen
            i += 3
            s_i += 3
            px[x, y] = (max(0, min(255, fr)),
                        max(0, min(255, fg)),
                        max(0, min(255, fb)))
    if stain_strength > 0:
        # Subtle darker ink stipple, tileable
        sd = ImageDraw.Draw(out)
        for _ in range(stains * 4):
            cx = rng.randint(0, w - 1)
            cy = rng.randint(0, h - 1)
            r = rng.randint(1, 2)
            sd.ellipse([cx - r, cy - r, cx + r, cy + r],
                       fill=tuple(max(0, c - stain_strength) for c in base_rgb))
    return out


def make_ink_border(size=(64, 64)):
    """1px brown ink stroke as a tileable ring (transparent center)."""
    w, h = size
    img = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    ink = (61, 40, 23, 220)  # #3D2817
    # 1px outer ring
    d.rectangle([0, 0, w - 1, h - 1], outline=ink)
    return img


# ---------- main ------------------------------------------------------------

PALETTE = {
    "parchment_base": (232, 208, 169),   # matches existing --paper-bg #E8D0A9
    "parchment_warm": (216, 188, 140),   # darker tan variant ~ --paper-dark
    "navy_header":    (38, 66, 100),     # sampled from mockup header
    "gold_cta":       (210, 165, 68),    # sampled from mockup CTA strip
    "ink":            (61, 40, 23),      # brown ink stroke
}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="client/assets/textures",
                    help="Output directory (default: client/assets/textures)")
    args = ap.parse_args()

    out_dir = args.out
    os.makedirs(out_dir, exist_ok=True)

    # 1. parchment-tile.png — 256x256 main paper grain
    rng = random.Random(SEED + 1)
    img = make_parchment((256, 256), rng, PALETTE["parchment_base"],
                         warmth=0, fiber_strength=12, stain_strength=18, stains=10)
    img.save(os.path.join(out_dir, "parchment-tile.png"), optimize=True)
    print("wrote parchment-tile.png", img.size)

    # 2. parchment-warm.png — 256x256 darker tan variant for nested panels
    rng = random.Random(SEED + 2)
    img = make_parchment((256, 256), rng, PALETTE["parchment_warm"],
                         warmth=2, fiber_strength=14, stain_strength=22, stains=12)
    img.save(os.path.join(out_dir, "parchment-warm.png"), optimize=True)
    print("wrote parchment-warm.png", img.size)

    # 3. wood-frame-9slice.png — 64x64 9-slice border (golden-brown bevel)
    img = make_wood_frame_9slice(size=(64, 64), border=14)
    img.save(os.path.join(out_dir, "wood-frame-9slice.png"), optimize=True)
    print("wrote wood-frame-9slice.png", img.size)

    # 4. navy-header-tile.png — 128x32 dark navy header strip
    rng = random.Random(SEED + 4)
    img = make_strip((128, 32), rng, PALETTE["navy_header"],
                     amp=10, stain_strength=18, stains=6)
    img.save(os.path.join(out_dir, "navy-header-tile.png"), optimize=True)
    print("wrote navy-header-tile.png", img.size)

    # 5. gold-cta-tile.png — 128x32 vivid gold strip
    rng = random.Random(SEED + 5)
    img = make_strip((128, 32), rng, PALETTE["gold_cta"],
                     amp=12, stain_strength=24, stains=8)
    img.save(os.path.join(out_dir, "gold-cta-tile.png"), optimize=True)
    print("wrote gold-cta-tile.png", img.size)

    # 6. ink-border.png — 64x64 1px brown ink stroke ring
    img = make_ink_border((64, 64))
    img.save(os.path.join(out_dir, "ink-border.png"), optimize=True)
    print("wrote ink-border.png", img.size)


if __name__ == "__main__":
    main()
