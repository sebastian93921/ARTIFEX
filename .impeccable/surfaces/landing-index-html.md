---
version: 1
slug: "landing-index-html"
primary_target: "landing/index.html"
related_targets: ["landing/ko/index.html"]
---

# Surface brief: ARTEX public landing page

Scope: `landing/index.html` (English, default) and `landing/ko/index.html`
static GitHub Pages at https://github.com/Autumn-27/ARTEX/. Mode:
Persuade. This brief is development-only context and is never shipped in
`landing/`.

Audience and job: developers and security researchers evaluating ARTEX
for locally isolated research. They should see the real application, understand
how a task moves across its actual screens, learn how a coding agent can drive
it, trust the stated limits, and reach the v0.1.0 release or setup commands.

Proof on hand: repo-owned fictional demo screenshots (en/ko, desktop 1440/960 +
mobile 390), README two-graph architecture, adapter README (8 tools, env vars,
install commands), release notes (5 platform archives + GHCR image), VERIFICATION
(one inherited failing test, npm audit findings, unverified Docker/Windows
runtime, adapter tests on local fixtures only), and an observed owner workflow.
The owner's real conversation was inspected in the signed-in local UI: task
creation from chat, exploration-graph and worker-trace inspection, and
progress updates. The page uses only that privacy-safe summary — no target
names, raw messages, vulnerability claims, token counts or private screenshots —
and frames it as orchestration evidence, not independent security validation.
No testimonials, metrics or benchmarks.

Constraints: plain HTML/CSS/JS, relative asset paths that resolve under
`/artex/` and `/artex/ko/` (OG images are absolute
`https://github.com/Autumn-27/ARTEX/assets/img/og-*.jpg` with `en_US`/`ko_KR`
locales, since crawlers need absolute URLs), self-hosted OFL fonts, no
trackers/forms/CDNs/frameworks, a demo caption on every screenshot, and no
contract/seed/planning text in shipped files.

## Direction contract

THESIS: Screenshot-led. One large, legible real screenshot makes the
application tangible in the first viewport; the page then varies structure and
density through a concrete screen-by-screen walkthrough, a full-bleed agent
bridge, a concrete install entry, requirements, and honest limits. Mechanism is
explained in prose beside real screens — no generated diagram, no
equal-feature-card grid.

OWN-WORLD: The app's console expanded to page scale — near-white #fafafa ground,
#171717 ink and logo-tile black, one restrained blue (#2563eb, hover #1b45c9;
#7cafff on the dark band) reserved for links, accents and focus. Hairline 1px
rules divide sections. A faint scope-grid motif, echoing the logo's 3x3 lattice,
sits behind the hero and the agent band. Schibsted Grotesk for voice, JetBrains
Mono for identifiers and commands; Korean uses the system Hangul stack with
keep-all word wrapping. Screenshots are framed in a restrained app-chrome bar.

STORY: Visitor meets the dashboard at full size, walks one task across five real
screens via an accessible selector (survey → explore → review → configure →
providers), learns how Claude Code/Codex/Pi drive it without replacing its
agents, copies setup commands for the self-hosted install, reads the plain
limits beside the deidentified owner workflow, and downloads v0.1.0.

FIRST VIEWPORT: Left 5/12 — headline, one-paragraph offer, Download v0.1.0
(primary) + Setup guide, stack/licence line. Right 7/12 — one large framed real
dashboard screenshot with a fictional-demo caption. Sticky header with nav,
EN/KO switch, GitHub (icon-only with accessible name on mobile; header items
never shrink down to 320px).

SIGNATURE INTERACTION: the walkthrough tablist. Selecting a screen cross-fades
the framed screenshot and expands that screen's explanation; inactive
explanations collapse to zero height. Vertical roving-tabindex keyboard model
(Up/Down/Home/End), ARIA tabs, reduced-motion respected. A second horizontal
tablist swaps the three agent install commands, each with a localised copy
button.

HONEST LIMITS: locally isolated use only; backend CI red on one inherited test;
no production or effectiveness claims; unverified Docker/Windows runtimes; npm
audit findings; adapter tests use local fixtures and live
coding-agent-to-provider execution is not independently verified (linked to
VERIFICATION).

FINISH: the integrity script, DESIGN.md, a demo caption on every shipping
screenshot, and one coordinator-owned browser confirmation batch (EN/KO at 1440
and 390px).
