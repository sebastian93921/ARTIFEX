# ARTEX landing — DESIGN.md

<!-- impeccable:design-schema 1 -->

Static GitHub Pages landing page for ARTEX. English is the default
(`landing/index.html`); Korean is a peer (`landing/ko/index.html`). Plain
HTML/CSS/JS, no framework, no CDN, no tracker, no backend. Served under the
project base `/artex/` (and `/artex/ko/`) with relative asset paths.

## Mode

Persuade. The surface is the product's shop window: a developer or security
researcher should grasp what ARTEX is, see it running, understand how a
task flows and how a coding agent drives it, trust the limits, and reach the
release or setup commands.

## Visual world

The application console, expanded to page scale.

- **Ground** `#fafafa`, **surface** `#ffffff`, sunk `#f4f4f3`.
- **Ink** `#171717` (also the logo tile and the full-bleed agent band), with
  `#3f3f3f` / `#595959` for secondary and tertiary text. On the dark band:
  `#fafafa` text, `#adb0b8` secondary (tinted neutral, ≥ 6:1 on `#171717`).
- **One restrained blue**, reserved for links, accents and focus: `#2563eb`,
  hover `#1b45c9` on light; `#7cafff` / `#a8c8ff` on the dark band. Link colours
  flow through `--link` / `--link-hover`, which the dark band re-points so hover
  stays legible on ink.
- **Hairlines** — 1px `#e6e6e4` / `#d4d4d2` rules separate every section; no
  heavy dividers.
- **Scope-grid motif** — a faint two-axis grid echoing the logo's 3×3 lattice
  sits behind the hero (radial-masked) and the agent band. This is the brief's
  committed brand motif; the detector flags decorative grids by default, and the
  committed world overrides that default here.
- **Depth** — restrained elevation shadows with real offset and soft blur; no
  zero-offset coloured halos. Screenshots sit in a light app-chrome frame.

## Type

- **Schibsted Grotesk** (self-hosted variable woff2, weights 400–900, OFL) for
  all voice: headings 640–660 weight, tracking −0.028em, balanced wrap. Display
  clamps to ≈3.7rem.
- **JetBrains Mono** (self-hosted variable woff2, OFL) for identifiers,
  commands, provenance and chips, with `tnum`/`zero` features.
- Latin faces only are self-hosted; the Korean edition layers a system Hangul
  stack (`Apple SD Gothic Neo`, `Pretendard`, `Malgun Gothic`, `Noto Sans KR`)
  behind the Latin display face, so ASCII reads in the same voice both ways.
  Korean wraps with `word-break: keep-all` + `overflow-wrap: break-word`;
  inline `<code>` identifiers in prose use `overflow-wrap: anywhere` so long
  test names wrap instead of widening grids (stacked grids use `minmax(0, 1fr)`).
- Body measure held to ~33–68ch; functional text ≥ 11px.

## Structure

Single column of hairline-separated sections, varying density deliberately:

1. **Hero** — split 5/7: offer copy + actions on the left, one large framed real
   dashboard screenshot on the right.
2. **Walkthrough** — 7/5 split. A vertical ARIA tablist (five real screens:
   Dashboard, Tasks, Findings, System settings, LLM templates) swaps a sticky
   framed screenshot and expands a one-line explanation per screen. Mechanism
   (two linked graphs, planner → single-intent workers → facts/findings, human
   review, retest) is explained in prose — there is no generated diagram.
3. **Agent bridge** — full-bleed ink band. A horizontal ARIA tablist swaps the
   Claude Code / Codex / Pi install commands, each with a copy button; the eight
   adapter tools and the honest "control interface, not a replacement" framing
   sit alongside a framed mobile screenshot.
4. **Install** — three concrete paths (one-click script, Docker Compose, release
   archive) with copyable commands and the five platform-archive chips + GHCR.
5. **Requirements** — four hairline cells: PostgreSQL, a separately configured
   model provider, Node.js 22+ for the adapter, a local lab target.
6. **Limits & credits** — a plain limits list (locally isolated only; backend CI
   red on one inherited test; no production/effectiveness claims; unverified
   Docker/Windows; npm audit findings; adapter tests use local fixtures and
   live coding-agent-to-provider execution is not independently verified, linked
   to VERIFICATION) beside the deidentified
   owner-workflow note and the ARTEX/AGPL-3.0 provenance + font/screenshot
   provenance.
7. **Footer** — brand, resource links, licence and "introduction, not a live
   service" note.

## Signature interaction

The walkthrough tablist. Selecting a screen cross-fades the framed screenshot
(opacity) and reveals its explanation via an animated `grid-template-rows`
collapse — one authored moment, not a per-section entrance. Roving tabindex with
Up/Down/Home/End; the horizontal agent tablist uses Left/Right.

## Accessibility & states

- Skip link, landmark regions, labelled nav, visible themed focus ring (blue on
  light, light-blue on dark).
- ARIA tabs with correct `aria-selected` / `tabindex` / `hidden`; keyboard
  operable; copy buttons announce localised state (`Copy`/`Copied` ·
  `복사`/`복사됨`) and degrade to manual selection without clipboard support.
- Mobile nav disclosure with `aria-expanded`. Header items never shrink; a
  ≤420px step tightens spacing down to 320px. The icon-only GitHub link keeps
  its visually hidden "GitHub" text as its accessible name.
- Themed browser surfaces: selection, scrollbar, focus ring, link underline
  offset, tabular mono numerals.
- `prefers-reduced-motion` neutralises transitions and smooth scrolling.
- Images carry width/height and `aspect-ratio` to avoid layout shift; the hero
  shot is `fetchpriority="high"`, the rest lazy where below the fold.

## Honesty & provenance

Every screenshot is captioned fictional demo data. The owner-workflow paragraph
is deidentified orchestration evidence only — no targets, findings, messages,
model usage or credentials. Claims trace to README, the adapter README,
PROVENANCE, VERIFICATION and the v0.1.0 changelog. ARTEX is a derivative of
ARTEX under AGPL-3.0; usage is restricted to locally isolated research.

## Verification performed (build-time, non-browser)

- `tools/validate-landing.mjs` (outside `landing/`): asset existence, relative
  base-path safety, EN/KO id/tab/control parity, language cross-links, demo
  captions, no planning metadata shipped, `.nojekyll` present — 17/17.
- `node --check` on the JS and the validation script.
- Impeccable detector: 19 → 4 residual (single-accent hover statically paired
  against the dark band; committed full-bleed band inset by its inner wrap) —
  both render correctly. Advisory grid motif is the committed brand world.
- Coordinator inspected English and Korean in Chrome at 1440px and 390px,
  applied one correction batch, then confirmed both languages also fit 320px
  without horizontal overflow. Screenshot tabs, agent tabs, keyboard switching,
  mobile menu, language links, and localized copy feedback passed. No browser
  console errors were observed. Below-fold lazy screenshots loaded on navigation.
- Pages deploys only `landing/`; the workflow runs file integrity and JavaScript
  syntax checks before upload. Development documents and private runtime data
  remain outside the published artifact.

## Implementation attribution

Claude Code was requested with `claude-opus-5-5` at medium effort. Its main
implementation session automatically fell back to `claude-opus-4-8` after a
provider safeguard interruption. The bounded browser-QA correction session ran
entirely on `claude-opus-5-5` at medium effort. The page is therefore not attributed
exclusively to Opus 5.5.
