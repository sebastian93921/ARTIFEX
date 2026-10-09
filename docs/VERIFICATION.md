# Verification and known limitations

Validation recorded on **2026-10-03** for the English-language ARTIFEX edition, based on upstream ARTEX commit `160fe13`. Tests use disposable PostgreSQL databases and local fixtures. This is source delivery; no binary release or container image has been published.

| Check | Result |
| --- | --- |
| Frontend regression and localization tests | 21 passed; TypeScript check passed |
| Production static export and embedded Go binary | Built successfully |
| Go regression suites, 18 packages | 986 tests/subtests passed; 1 inherited test failed; 1 external-provider test skipped |
| Complete server suite | 285 tests/subtests passed; only the opt-in live-provider test skipped |
| Backend translation catalog | 1,865 production keys passed completeness checks |
| Authored Chinese text audit | No untranslated authored source/docs or Go comments; deliberate compatibility/data exceptions retained |
| Desktop browser coverage | 24 routes in each language; no missing labels or page-level horizontal overflow |
| Mobile browser coverage | Dashboard, tasks, findings and settings in both languages; no page-level horizontal overflow at 390px |
| Real application screens | Login and notification forms checked in both languages; setup validation and initialization verified |
| Language switching | Navigation/status labels update; selection and unsent drafts are preserved |
| HTTP localization | Markdown/CSV export, language negotiation, saved preference and invalid-language rejection checked |
| GLM-5.3 provider contract | 21 focused tests/subtests passed with local HTTP/SSE fixtures; both endpoint paths, reasoning and tool-call replay covered |
| GLM template browser flow | Both templates checked in both languages; entered name/key/proxy preserved; no save, activation or provider request |
| Packaging and launch scripts | Shell syntax, bilingual packaging fixtures and 10 supervisor cases passed |
| Provenance and dependencies | Original license unchanged; dependency versions and Go module unchanged; documentation links and diff checks passed |

Counts include Go parent tests and their subtests. Package runs use separate databases initialized with the same supplementary metering tables as normal server startup. The original tests and checks remain enabled.

## Inherited issues

`TestGraphOverviewExpandsAssociatedCompanyScope` still fails with `task scope missing: <nil>`. The same failure was reproduced in untouched upstream source. It is not excluded or marked successful, so the complete backend CI remains red until that separate issue is fixed.

`TestTaskMetadataPatchReturnsRenameAndPin` previously produced an intermittent temporary-directory cleanup failure in upstream verification. It passed in the final ARTIFEX server run; no unrelated lifecycle change was made to hide it.

The upstream mock preview has no notification metadata mock, so its Notifications page errors. The **real backend Notifications page and add-channel form pass** in both languages. No notification was sent during verification.

The unchanged npm dependency set reports **14 audit findings: 1 critical, 9 high and 4 moderate**. Dependency upgrades remain separate work.

## Scope and limits

- The optional `TestLiveContextReview` requires private external-provider configuration and was not run. No paid model calls, external target scans, or real notification deliveries were performed.
- GLM tests prove the local request/response contract, not account access or provider availability. Coding Plan is restricted to officially supported tools, and ARTIFEX is not listed; see [provider setup](llm-providers.md).
- Docker image execution and Windows batch execution were not verified. Packaging fixtures do not claim a real Windows runtime test.
- Setup reached the authenticated route, but one browser wait for the window load event timed out; initialization and the new password were then confirmed through the real API.
- User-entered text, stored evidence, edited prompts, wire identifiers, legacy parsing markers, Unicode fixtures, and original upstream screenshots retain their original content. The app does not translate arbitrary user data or third-party tool output.
- Historical validation documents under `sidequestion/` describe upstream work, not new ARTIFEX runs.

Current remote checks are available in [GitHub Actions](https://github.com/sebastian93921/ARTIFEX/actions/workflows/ci.yml).
