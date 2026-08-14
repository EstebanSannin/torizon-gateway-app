# Torizon Gateway — Design System

Derived from the official **Torizon.io** brand guidelines (Colors + Typography) and the official logo/avatar artwork. This is the human-readable reference; the machine source of truth for the UI is [`web/static/css/tokens.css`](../web/static/css/tokens.css). **Never hard-code a color or font in a template — reference a token.**

## 1. Logo & mark

Official assets are vendored in [`web/static/brand/`](../web/static/brand/):

| File | Use |
|------|-----|
| `torizon-avatar.svg` | The hexagonal cube "T" mark (square). App icon, favicon, compact header. |
| `torizon-avatar-reverse.svg` | Blue-reverse avatar for dark/colored backgrounds. |
| `torizon-logo.svg` | Full Torizon wordmark — light backgrounds. |
| `torizon-logo-dark.svg` | Full wordmark for dark backgrounds. |
| `torizon-logo-tagline.svg` | Wordmark + tagline. |
| `torizon-gateway-logo.svg` | **Product lockup** — Torizon mark + wordmark + "Gateway" descriptor. Light backgrounds. |
| `torizon-gateway-logo-dark.svg` | Product lockup for dark backgrounds (used in the app sidebar). |

**Mark colors** (from the avatar artwork — note these differ slightly from the web palette):
`#0096DC` (blue), `#00508C` (dark blue), `#FAAF00` (amber). Tokens: `--tn-logo-*`.

### "Torizon Gateway" product lockup
Built as `torizon-gateway-logo[-dark].svg`: the **official** Torizon mark + wordmark (unmodified vector paths) followed by a thin divider and the descriptor **"Gateway"** set in **Inter Regular** (the brand typeface), baseline-aligned to the wordmark. This master-brand + descriptor pattern keeps the official logo intact while identifying the product. "Gateway" is live `<text>` — it renders with the app's vendored Inter; if the lockup is reused where Inter isn't loaded it falls back to `system-ui`. Outline it to paths if a fully font-independent asset is ever needed.

## 2. Color palette

### Primary (brand blue)
| Token | Hex | Pantone |
|-------|-----|---------|
| `--tn-primary` | `#001F3F` | 282 C |
| `--tn-primary-light` | `#0092FF` | 299 C |
| `--tn-primary-medium` | `#003266` | |
| `--tn-primary-dark` | `#00172E` | |

### Secondary (amber)
`--tn-secondary #FFC107` · `--tn-secondary-light #FFD557` · `--tn-secondary-medium #FAA500` · `--tn-secondary-dark #FA8700`

### Tertiary (indigo)
`--tn-tertiary #2F39B2` (7548 C) · `--tn-tertiary-light #7A81DC` · `--tn-tertiary-medium #4954CF` · `--tn-tertiary-dark #202779`

### Text
`--tn-text-primary #1A4775` · `--tn-text-primary-dark #00172E` · `--tn-text-primary-light #99ADC2`

### Backgrounds / tints
`#D6EDFF` light-blue · `#FFF2CC` light-orange · `#EBECF9` light-indigo · `#E5FBFF` light-gray · `#F8F8F8` lightest-gray · `#BFBFBF` medium-gray · `#A6A6A6` normal-gray · `#666666` dark-gray · plus black & white.

### Semantic roles (what the UI actually uses)
`--color-bg`, `--color-surface`, `--color-border`, `--color-text`, `--color-heading`, `--color-accent`, `--color-highlight`, `--color-sidebar-*`. These are remapped for dark mode; the raw `--tn-*` values never change.

## 3. Typography

- **Typeface: Inter** (Google Fonts). Weights: **Light 300**, **Regular 400**, **Bold 700**. Vendor the WOFF2 files locally under `web/static/vendor/inter/` (offline device — no Google Fonts CDN). Falls back to `system-ui`.
- **Body:** Regular 400, 18px / 1.125rem, line-height 1.5.
- **Small:** 16px / 1rem.
- **Headings:** Bold 700 with letter-spacing **-1px** for display/H1–H2; 0 for smaller headings.
- **Display:** Light 300 for large display sizes.

Scale (from the brand guide): Display 84/72/60/48/36/30px (Light) · H1 48 · H2 36 · H3 30 · H4 24 · H5 18px (Bold).

## 4. Principles for this UI
- **Clean, information-dense, calm.** It's an industrial device console, not a marketing page.
- **Light mode is the default;** dark mode supported via `data-theme="dark"` (brand ships dark logo variants).
- **Accent = brand blue** (`--color-accent`), amber reserved for warnings/highlights.
- **Everything vendored & embedded** — no external fonts, CSS, or JS. Works air-gapped.
