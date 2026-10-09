#!/usr/bin/env node
/* Integrity check for the ARTEX landing page.
   Lives outside landing/ so it is never published. Verifies that every
   referenced asset exists, that relative paths resolve under /artex/,
   that the site stays English-only (no stale /ko/ links), and that no
   planning/seed/contract metadata leaked into the shipped folder. */

import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const landing = join(root, "landing");
let errors = 0;
let checks = 0;

const fail = (m) => {
  errors++;
  console.error("  ✗ " + m);
};
const ok = (m) => {
  checks++;
  console.log("  ✓ " + m);
};

/* --- 1. Referenced assets resolve relative to each HTML file --- */
const pages = [
  { file: join(landing, "index.html"), base: landing, lang: "en" },
];

const attrRe = /(?:src|href)="([^"#?][^"]*)"|srcset="([^"]+)"/g;

for (const { file, base, lang } of pages) {
  if (!existsSync(file)) {
    fail(`missing page: ${file}`);
    continue;
  }
  const html = readFileSync(file, "utf8");

  // lang attribute
  if (!new RegExp(`<html lang="${lang}"`).test(html))
    fail(`${lang}: <html lang="${lang}"> missing`);
  else ok(`${lang}: html lang is "${lang}"`);

  let m;
  const refs = new Set();
  while ((m = attrRe.exec(html))) {
    if (m[1]) refs.add(m[1]);
    if (m[2]) m[2].split(",").forEach((s) => refs.add(s.trim().split(/\s+/)[0]));
  }
  for (const ref of refs) {
    if (/^(https?:|mailto:|data:|\/\/)/.test(ref)) continue; // external/absolute
    const clean = ref.replace(/[#?].*$/, "");
    if (clean === "" || clean === "./" || clean === "../") continue;
    const target = resolve(base, clean);
    // must stay inside landing/
    if (!target.startsWith(landing)) {
      fail(`${lang}: reference escapes landing/: ${ref}`);
      continue;
    }
    if (!existsSync(target)) fail(`${lang}: missing asset -> ${ref}`);
  }
  ok(`${lang}: ${refs.size} references scanned, all local assets present`);

  // no absolute-root asset paths (would break under /artex/ base)
  if (/(?:src|href)="\/(?!\/)/.test(html))
    fail(`${lang}: absolute root path found (breaks project-pages base)`);
  else ok(`${lang}: no absolute root asset paths`);

  // English-only: no links back to the removed /ko/ tree
  if (/href="ko\/"/.test(html) || /href="\.\.\/ko\/"/.test(html))
    fail(`${lang}: stale link to removed ko/ page`);
  else ok(`${lang}: no links to ko/`);
}

/* --- 2. Every screenshot figure carries a demo caption --- */
const en = readFileSync(join(landing, "index.html"), "utf8");
const figs = (en.match(/<figure/g) || []).length;
const caps = (en.match(/figcaption class="demo"/g) || []).length;
if (caps < figs) fail(`en: ${figs} figures but only ${caps} demo captions`);
else ok(`en: ${caps} demo captions for ${figs} figures`);

/* --- 3. No planning/seed/contract metadata shipped in landing/ --- */
const banned = [/THESIS:/, /OWN-WORLD:/, /surface seed/i, /Direction contract/i, /impeccable:product-schema/];
const walk = (dir) => {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    const st = statSync(p);
    if (st.isDirectory()) walk(p);
    else if (/\.(html|css|js|txt|xml|json|md)$/.test(entry)) {
      const txt = readFileSync(p, "utf8");
      for (const re of banned)
        if (re.test(txt)) fail(`planning metadata leaked into ${p} (${re})`);
    }
  }
};
walk(landing);
ok("no planning/seed/contract metadata found in landing/");

/* --- 4. .nojekyll present --- */
if (!existsSync(join(landing, ".nojekyll"))) fail(".nojekyll missing");
else ok(".nojekyll present");

console.log(`\n${checks} checks passed, ${errors} error(s).`);
process.exit(errors ? 1 : 0);
