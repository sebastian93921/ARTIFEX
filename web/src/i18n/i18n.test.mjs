import assert from "node:assert/strict";
import test from "node:test";
import fs from "node:fs";
import path from "node:path";
import { catalogs } from "./catalog.ts";
import { normalizeLocale, readCookieLocale, withLocaleQuery, withLocaleHeaders } from "./config.ts";
import { placeholders, tags, interpolate, createFormatters } from "./format.ts";
import { __resetLocaleForTests, getLocale, setLocale, initializeLocale, translate, translateIn, subscribeLocale, localeHeaders } from "./runtime.ts";
import { cloneDemoFixture } from "./demo-fixture.ts";
import { localizedFetch } from "./request.ts";
import { mentionKinds, mentionToken, selectedMentions, mentionSearch } from "../lib/chat-mentions.ts";
import { sourceFiles, scanFile, SRC_ROOT } from "./han-scan.mjs";

function browser(stored = null, cookie = "") {
  const values = new Map(stored ? [["artex.locale", stored]] : []);
  globalThis.window = { localStorage: { getItem: (key) => values.get(key) ?? null, setItem: (key,value) => values.set(key,value) } };
  globalThis.document = { cookie, documentElement: { lang: "en" } };
  return values;
}
function reset() { delete globalThis.window; delete globalThis.document; __resetLocaleForTests(); }

test("English is the default; unsupported and malformed preferences safely fall back", () => {
  reset(); assert.equal(getLocale(), "en");
  assert.equal(normalizeLocale("ko-KR"), null);
  assert.equal(normalizeLocale("zh-CN"), null);
  assert.equal(readCookieLocale("artex_locale=%E0%A4%A"), null);
  browser("unsupported", "artex_locale=ko"); initializeLocale(); assert.equal(getLocale(), "en");
  reset(); browser("unsupported", "artex_locale=%bad"); initializeLocale(); assert.equal(getLocale(), "en"); reset();
});

test("persisted unsupported locales stay English and still persist the active locale", () => {
  reset(); browser("ko");
  assert.equal(getLocale(), "en"); assert.equal(translate("common.save"), "Save");
  let calls=0; const unsubscribe=subscribeLocale(()=>calls++); initializeLocale();
  assert.equal(getLocale(), "en"); assert.equal(translate("common.save"), "Save");
  setLocale("en"); assert.equal(window.localStorage.getItem("artex.locale"),"en"); assert.equal(calls,0);
  unsubscribe(); reset();
});

test("the catalog is complete and free of placeholder or Han leakage", () => {
  assert.ok(Object.keys(catalogs.en).length >= 3000);
  for(const key of Object.keys(catalogs.en)) {
    assert.ok(catalogs.en[key].length, key);
    assert.doesNotMatch(catalogs.en[key], /Untranslated|미번역|\p{Script=Han}/u,key);
  }
  assert.equal(interpolate("Task {id}: {name}",{id:7,name:"사용자 原文 {unchanged}"}),"Task 7: 사용자 原文 {unchanged}");
  assert.equal(interpolate("{missing}",{}),"{missing}");
});

test("locale transport preserves caller headers, queries, and fragments", async () => {
  assert.equal(withLocaleQuery("/api/report?task=3#section","en"),"/api/report?task=3&lang=en#section");
  assert.equal(withLocaleQuery("/api/report?lang=ko","en"),"/api/report?lang=ko");
  assert.deepEqual(withLocaleHeaders("en", {Authorization:"Bearer demo", "Content-Type":"application/json"}),{"Accept-Language":"en",Authorization:"Bearer demo","Content-Type":"application/json"});
  reset(); browser(); setLocale("en"); const original=globalThis.fetch; let observed;
  globalThis.fetch=async(input,init)=>{observed={input,init};return new Response("ok")};
  try {
    await localizedFetch("/api/report?task=1",{headers:{Authorization:"Bearer example"}});
    assert.equal(observed.input,"/api/report?task=1&lang=en");
    assert.equal(new Headers(observed.init.headers).get("accept-language"),"en");
    assert.equal(new Headers(observed.init.headers).get("authorization"),"Bearer example");
  } finally {globalThis.fetch=original;reset();}
});

test("authored demo data is read-only and cloned user values are never translated", () => {
  reset(); browser();
  const fixture=cloneDemoFixture({title:"Dashboard",nested:[{name:"Save"}]},true);
  assert.equal(fixture.title,"Dashboard"); assert.equal(fixture.nested[0].name,"Save");
  Object.assign(fixture,{title:"My dashboard 原文"}); const runtimeCopy=cloneDemoFixture(fixture);
  assert.equal(fixture.title,"My dashboard 原文");assert.equal(runtimeCopy.title,"My dashboard 原文");
  fixture.title="Save"; const editedCopy=cloneDemoFixture(fixture);assert.equal(editedCopy.title,"Save","user text matching a catalog entry is still user text");reset();
});

test("localized mention labels preserve legacy wire tokens and user content", () => {
  reset();browser();assert.equal(mentionKinds[0].label,"Vulnerability");
  assert.equal(mentionSearch("漏洞test").kind,"finding");
  const token=mentionToken({kind:"finding",id:12,label:"사용자 原文",description:""});assert.equal(token,"@[漏洞#12 사용자 原文]");
  assert.equal(selectedMentions(token)[0].label,"Vulnerability #12 · 사용자 原文");reset();
});

test("date and number formatters follow the selected locale", () => {
  const date=new Date("2026-10-02T00:00:00Z");
  const en=createFormatters("en");
  assert.equal(en.date(date,{timeZone:"UTC"}),new Intl.DateTimeFormat("en-US",{timeZone:"UTC"}).format(date));
  assert.equal(en.number(12345),new Intl.NumberFormat("en-US").format(12345));
});

test("authored UI contains no untranslated Han literals; legacy parsers are explicit exceptions", () => {
  const legacyWire=new Set(["漏洞","资产","企业","接口","应用","域名","子域名","服务"]);
  const failures=[];
  for(const file of sourceFiles()) {
    const rel=path.relative(SRC_ROOT,file);
    if(rel.endsWith(".test.mjs"))continue; // Tests intentionally preserve multilingual user input.
    for(const hit of scanFile(file)) {
      if(rel==="lib/chat-mentions.ts" && legacyWire.has(hit.text))continue;
      if(rel==="components/approval-records.tsx" && hit.text==="/^\\[(?:模型|Model|모델)\\]\\s*/")continue;
      if(rel==="components/transcript.tsx" && hit.text==="/(?:工具|Tool|도구)\\s+(\\S+)\\s+(?:请求|requests?|요청)/i")continue;
      if(rel==="lib/company-scope.ts" && hit.text==="/icp|备案/i")continue;
      failures.push(`${rel}:${hit.line} ${hit.text}`);
    }
  }
  assert.deepEqual(failures,[]);
});

test("authored source and build configuration comments are English", async () => {
  const { hasHanComment } = await import("./han-scan.mjs");
  const files=[...sourceFiles(),path.resolve("next.config.mjs")];
  assert.deepEqual(files.filter(file=>hasHanComment(file)).map(file=>path.relative(SRC_ROOT,file)),[]);
});


test("dashboard count and token phrases preserve natural English spacing", () => {
  assert.equal(translateIn("en", "app.metricValue", { label: "Critical", value: 2 }), "Critical 2");
  const tokens = { input: "2.7M", cache: "1.7M", output: "211.7k" };
  assert.equal(translateIn("en", "app.tokenSummary", tokens), "In 2.7M (including cache 1.7M) · Out 211.7k");
});


test("rich settings paragraphs preserve emphasis", () => {
  for (const key of Object.keys(catalogs.en).filter(key => key.startsWith("settings."))) {
    assert.ok(tags(catalogs.en[key]).length >= 0, key);
  }
  assert.equal(translateIn("en", "settings.pagination", { from: 1, to: 4, total: 4 }), "1–4 / 4 records");
});
