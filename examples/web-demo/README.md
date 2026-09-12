# omniviz example — web driver (Chromatic-style captures)

The web driver keeps **one warm headless browser** for the whole run and
opens an isolated tab per shot — tabs cost milliseconds, browser processes
cost seconds. Per shot it:

1. navigates (URL, or a project-relative path served as `file://`)
2. injects a freeze stylesheet: animations pause in place, transitions and
   caret blinking off, smooth scrolling off
3. waits for `document.readyState === 'complete'` **and**
   `document.fonts.status === 'loaded'` (the classic wrong-font flake)
4. optionally polls your `wait_ready` JS expression
5. screenshots the viewport (`full_page = true` for the scrollable page)

Steps 2–3 are what makes captures deterministic enough to diff — this demo
page animates continuously (pulsing dot, animated SVG ring) and still
produces byte-identical PNGs on every run.

```bash
omniviz test        # PASS 2 · FAIL 1 — the failure ships by design
omniviz review
```

| shot | what it shows |
|---|---|
| `dashboard` | animated dashboard — **passes**, deterministically |
| `audit` | second page, different accent — **passes** |
| `broken` | same dashboard with a stat card deleted and the accent hue shifted — **fails** (the "deploy quietly ate the layout" regression) |

## Browser resolution

`[driver.web] browser=` → `$OMNIVIZ_BROWSER` → chromedp auto-detect
(Chrome/Chromium/Edge in standard locations). On hardened distros or in
root containers where the chrome sandbox can't start, the driver falls back
to `--no-sandbox` automatically and remembers it for the rest of the run
(force it with `sandbox = false` in config).

Performance knobs live in `[driver.web]` / `driver_options`:
`browser`, `headless` (default true), `full_page`, `wait_ms` (settle,
default 250), `wait_ready` (JS predicate), `freeze` (default on),
`sandbox`.
