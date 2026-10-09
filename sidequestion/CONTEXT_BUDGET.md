# Long side conversations and context budgets

[한국어](../docs/ko/sidequestion/CONTEXT_BUDGET.md)

Historical upstream fix, 2026-09-11. The original implementation treated request JSON characters as tokens and inherited the main task's 32K output reservation. It therefore rejected valid side questions too early when HTML, JavaScript and tool results occupied much of the context.

## Open-source implementations reviewed

- [Grok CLI side context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): extracts recent user/assistant text with an approximately 2000-character budget and up to 400 characters per message. This path does not maintain ongoing side-question history.
- [Grok CLI independent request](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): independent cancellation signal, 2048-token output cap when supported, no tools.
- [Grok CLI main-session compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): estimates tokens, keeps recent content, updates old summaries with new material and handles truncation across turns.
- [OpenCode session compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): estimates the full request, reserves output/buffer space, combines recent content and rolling summaries, and makes summary requests without tools. Defaults in that implementation are 8000 recent-context tokens and 4096 summary-output tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): attempts recovery only before assistant output begins, and recovered calls do not re-enter the same recovery path.

ARTIFEX borrowed separate output budgets, recent content plus rolling summaries, and bounded recovery. It retained norma v0.3.6 structured messages and tool pairing rather than Grok's text excerpts. OpenCode-style main-session compaction events were not written to ARTIFEX's main transcript. ARTIFEX retains this design; cited versions describe the historical implementation.

## Request budget and execution

- Messages use norma's content-block UTF-8 byte estimate with a 4/3 margin, plus system instructions, tool schemas and message-wrapper overhead. This is an estimate, not an exact model token count.
- Side output defaults to at most 8192 tokens and never exceeds the main configuration's output cap. `ARTIFEX_BTW_MAX_OUTPUT_TOKENS` accepts a server-side cap from 256 to 32768. It does not change the default model or main-task parameters.
- Input budget is context window minus output cap and safety margin. Unknown windows use the platform default of 200K. Safety margin is 5% of the window, bounded to 128–8192 tokens.
- Successful exchanges load in increasing ordinal batches of at most 20. At most 20 original exchanges are retained, using no more than one quarter of input budget or 16K tokens.
- Excess exchanges update a rolling summary with historical provenance and context time. Old assistant answers are not new tool evidence; conflicts prefer the latest main snapshot.
- If main context is still too long, only old messages in the copy are summarized. Up to 8K recent tokens remain. Cut points preserve tool-call/result pairs; an oversized group enters the summary as a whole.
- Summary input is split at UTF-8-safe boundaries according to remaining window space; output is capped at 2048 tokens. Empty, truncated, tool-call or over-budget summaries are not cached. A side request allows at most 12 summary calls within the same 120-second timeout. Reaching the cap fails explicitly instead of looping indefinitely.
- A first model context-overflow error before text or tool-call output permits one smaller retry. Recovery stops immediately if the estimated size did not decrease. Other model errors and partial streams do not trigger this recovery.
- All returned usage, including summaries, failed attempts and cancellation, accumulates on the same side request. When the provider returns no usage, only zero can be recorded; estimates must not masquerade as measured usage.

## Persistence and interface

`side_question_sessions.memory` stores summaries of older exchanges, covered ordinals and main-context summaries cached by snapshot identity. `side_question_requests.context_info` stores preparation stage, actual replay count, summary use and budget estimates.

Summaries save only while the original request is running and its clear version matches. Clear removes caches too; late writes cannot restore cleared data. New snapshots do not reuse old snapshot summaries. Archive v3 preserves summary fields. Restoring old v3 archives without these fields fills empty objects; v1/v2 remain compatible.

POST accepts and returns the request before background preparation/compaction, holding neither admission locks nor database transactions during that work. SSE/history shows preparing, organizing exchanges, compacting the copy and answering. Compaction failure persists as the side request's terminal failure. The frontend retains the error and restores the failed question as a draft, without a toast over the input. History polling no longer clears submission errors.

## Historical verification record

These are upstream results recorded for the 2026-09-11 fix, not new ARTIFEX results.

- Replay of 19/20/21/50 exchanges, retention of conclusions older than 20 exchanges, and summary-cache reuse after restart: automated checks passed.
- Very long Chinese answers/code context, chunk budgets, tool pairing, unchanged snapshots and cache invalidation for new snapshots: automated checks passed.
- Summary failure/cancellation/truncation/overlength/tool returns, clear races, call cap, single overflow recovery and no retry after partial streaming: automated checks passed.
- Isolated PostgreSQL pagination, restart, v1/v2/v3 archives, summary/budget metadata restore, old v3 missing fields and 20 parents sharing four concurrent slots: passed.
- Candidate Go build, frontend TypeScript, Biome for changed components and Next.js production build in an isolated directory: passed.
- In-app browser with an isolated UI fixture at 1280×720 and 390×844 verified preparation, summary-range hints, failed-draft restoration, no error toast, no horizontal overflow and no console errors. Temporary fixtures were removed.
- Read-only replay of existing local Worker snapshots passed the new budget check. For example, Worker #3's 293085-character snapshot was no longer rejected by local character counting. No external model call was made for this check.
- Automatic approval review rejected sending private Worker snapshots to Grok for a real conversation. That test was not run or counted as passing.
- At the user's request, the local backend restarted at 2026-09-11 00:37 with the original database, data directory and login configuration. The running file matched the candidate binary SHA-256; backend and frontend-proxied `/api/health` both returned healthy.

Verification commands (isolated test databases only):

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artifex
npx tsc --noEmit
npm run build -- --webpack
```

The historical frontend production build used a separate copy to avoid overwriting the active preview's `.next`. The candidate was `/private/tmp/artifex-btw-budget-candidate`, copied to `/private/tmp/artifex-btw-preview/artifex` and started. The previous binary was backed up there as `artifex.before-context-budget`. These paths describe that run, not files distributed with ARTIFEX.
