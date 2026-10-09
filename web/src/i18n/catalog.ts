// Assembles the per-namespace message modules into flat en/ko catalogs.
// Keys are "<namespace>.<key>". Every namespace module types its Korean table
// as Record<keyof typeof en, string>, so a missing or extra Korean entry is a
// compile error; i18n.test.mjs additionally checks placeholder/tag parity.

import type { Locale } from "./config.ts";
import * as providers from "./messages/providers.ts";
import * as app from "./messages/app.ts";
import * as interfaceMessages from "./messages/interface.ts";
import * as english from "./messages/english.ts";
import * as settings from "./messages/settings.ts";
import * as common from "./messages/common.ts";

const namespaces = {
  providers,
  settings,
  english,
  interface: interfaceMessages,
  app,
  common,
};

type Namespaces = typeof namespaces;
type NamespaceName = keyof Namespaces & string;

export type MessageKey = {
  [N in NamespaceName]: `${N}.${keyof Namespaces[N]["en"] & string}`;
}[NamespaceName];

export type Catalog = Record<MessageKey, string>;

function flatten(locale: Locale): Catalog {
  const out: Record<string, string> = {};
  for (const [ns, mod] of Object.entries(namespaces)) {
    const tables = mod as unknown as Record<string, Record<string, string> | undefined>;
    const base = mod.en as Record<string, string>;
    const table = tables[locale];
    for (const [key, value] of Object.entries(base)) {
      // zh falls back to the English value per key until its table provides one.
      out[`${ns}.${key}`] = (table && table[key]) || value;
    }
  }
  return out as Catalog;
}

export const catalogs = { en: flatten("en"), zh: flatten("zh") } as const;

export const namespaceNames = Object.keys(namespaces);
