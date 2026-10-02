# Akili brand

**Keyhole**: a lowercase *a* with a keyhole in its bowl. *Akili* is Swahili for intelligence; the keyhole
says that intelligence works under lock and key: default deny, explicit allow, always auditable.

## Files

| File | Use |
|---|---|
| `web/public/logo-icon.svg` (= `favicon.svg`) | App icon: forest ink tile, white mark, amber keyhole |
| `web/public/logo-mark.svg` | Mark on light backgrounds |
| `web/public/logo-mark-light.svg` | Mark on dark backgrounds |
| `web/public/favicon.png`, `apple-touch-icon.png` | Raster icons (64 px, 180 px full-bleed) |
| `docs/brand/akili-logo.png`, `akili-logo-dark.png` | Horizontal lockup for documents, light and dark |
| `docs/brand/akili-icon-512.png` | App icon for listings (e.g. a marketplace) |

The SVGs are the source; the PNGs are rendered from them.

## Colour

| Token | Hex | Use |
|---|---|---|
| Forest ink | `#0F2A24` | The mark on light backgrounds, the app icon tile |
| Keyhole amber | `#E9A23B` | The keyhole, always |
| White | `#FFFFFF` | The mark on dark backgrounds |

The console UI keeps the Miabi orange theme (`web/src/styles.css`); amber sits close to it.
The keyhole is always amber; the rest of the mark is forest ink on light and white on dark.
Never recolour the keyhole, add gradients, outline the mark or set the wordmark in another typeface.

## Type

- **Manrope** (ExtraBold 800, tracking −3%): the lowercase wordmark *akili* and page titles.
- **IBM Plex Sans**: interface and body text.

Both are bundled with the UI (no font CDN), so the console works offline.

## Clear space and size

Keep clear space of half the mark's height around it. Below 24 px, use the app icon rather than the bare mark.
