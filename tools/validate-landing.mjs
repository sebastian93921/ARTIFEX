#!/usr/bin/env node
/* Integrity check for the ARTEX landing page.
   Lives outside landing/ so it is never published. Verifies that every
   referenced asset exists, that relative paths resolve under /artex/
   and /artex/ko/, that EN/KO structural parity holds, and that no
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
  { file: join(landing, "ko", "index.html"), base: join(landing, "ko"), lang: "ko" },
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
}

/* --- 2. EN/KO parity on structural anchors and controls --- */
const en = readFileSync(join(landing, "index.html"), "utf8");
const ko = readFileSync(join(landing, "ko", "index.html"), "utf8");

const ids = (s) =>
  [...s.matchAll(/\bid="([^"]+)"/g)].map((x) => x[1]).sort();
const enIds = ids(en);
const koIds = ids(ko);
const missingInKo = enIds.filter((i) => !koIds.includes(i));
const missingInEn = koIds.filter((i) => !enIds.includes(i));
if (missingInKo.length || missingInEn.length) {
  fail(`id parity mismatch: ko-missing=[${missingInKo}] en-missing=[${missingInEn}]`);
} else ok(`EN/KO id parity (${enIds.length} ids each)`);

const countRole = (s, role) =>
  (s.match(new RegExp(`role="${role}"`, "g")) || []).length;
for (const role of ["tab", "tabpanel", "tablist"]) {
  if (countRole(en, role) !== countRole(ko, role))
    fail(`${role} count differs EN(${countRole(en, role)}) KO(${countRole(ko, role)})`);
  else ok(`EN/KO ${role} count matches (${countRole(en, role)})`);
}

const copyEn = (en.match(/class="copy-btn"/g) || []).length;
const copyKo = (ko.match(/class="copy-btn"/g) || []).length;
if (copyEn !== copyKo) fail(`copy-btn count differs EN(${copyEn}) KO(${copyKo})`);
else ok(`EN/KO copy-btn count matches (${copyEn})`);

// language cross-links
if (!/href="ko\/"/.test(en)) fail("EN page missing link to ko/");
else ok("EN links to ko/");
if (!/href="\.\.\/"/.test(ko)) fail("KO page missing link to ../ (EN)");
else ok("KO links to ../ (EN)");

/* --- 3. Every screenshot figure carries a demo caption --- */
for (const [name, s] of [["en", en], ["ko", ko]]) {
  const figs = (s.match(/<figure/g) || []).length;
  const caps = (s.match(/figcaption class="demo"/g) || []).length;
  if (caps < figs)
    fail(`${name}: ${figs} figures but only ${caps} demo captions`);
  else ok(`${name}: ${caps} demo captions for ${figs} figures`);
}

/* --- 4. No planning/seed/contract metadata shipped in landing/ --- */
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

/* --- 5. .nojekyll present --- */
if (!existsSync(join(landing, ".nojekyll"))) fail(".nojekyll missing");
else ok(".nojekyll present");

console.log(`\n${checks} checks passed, ${errors} error(s).`);
process.exit(errors ? 1 : 0);
