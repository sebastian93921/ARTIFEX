# Interface localization

English is the only interface language. The React locale provider restores an explicit English preference after hydration, keeping the static English export and first client render consistent. The selection persists in `artifex.locale` localStorage and the `artifex_locale` cookie. API transport reads the stored preference even before hydration finishes.

Use `useI18n().t` in components and `translate` in non-React helpers. Keep placeholders identical and pass user values as interpolation parameters. Never translate user-entered text, persisted reports, identifiers, API field names, credentials, or protocol values. Existing Chinese mention wire labels and historical parser aliases remain compatible with the backend; visible labels are localized independently.

The `interface` namespace contains the migrated interface and authored demo messages, with stable IDs. The `english` namespace covers pre-existing English interface labels. New features should use descriptive keys in an appropriate namespace. Static configuration objects expose localized labels through getters. Consumers subscribe to the locale, and memoized derived labels include the locale dependency. Language changes do not remount the application or clear drafts.

`localizedFetch` adds locale headers and query parameters while retaining caller headers. SSE URLs use `sseUrl`, which adds the locale query parameter. Use `getIntlLocale` or the `fmt` helpers for dates and numbers.

`prepareDemoFixture` runs only on authored initial demo fixtures. It makes their text reactive. Editing a fixture replaces its accessor with an ordinary writable value; `cloneDemoFixture` preserves this distinction, so edits are never retranslated, even when their text matches a catalog entry.

Run `npm test` from `web` for existing regression tests plus catalog parity, interpolation, persistence/hydration, transport, editable fixtures, legacy mention compatibility, formatting, and AST-aware Han coverage. The coverage test allows only explicitly documented legacy parsing and multilingual test data. It also checks that source and build-configuration comments are English. Run `npx tsc --noEmit` and `npm run build:static` for compilation and export verification.
