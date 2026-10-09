# `/btw` verification record

[한국어](../docs/ko/sidequestion/VALIDATION.md)

Historical upstream record. Date: 2026-09-10. Branch: `codex/btw-side-question`. Baseline: `8dae851b9b622f2ff2631f332fde9719d0b16fba`. These are not new ARTEX results.

Used an isolated PostgreSQL test database and data directory. Real-model credentials were injected only into that environment, never source or this record. The product default model was unchanged. Go 1.26.3, norma v0.3.6, Next.js 16.2.9.

Model conversations, returned objects, engineering assertions and the Qwen review are in [validation-2026-09-10.json](validation-2026-09-10.json), without API credentials. Narrative strings are translated; identifiers and measured values are retained.

## Engineering checks

| Scope | Result | Evidence |
| --- | --- | --- |
| Deep copies of structured messages and tool arguments | Passed | `TestCheckpointDeepCopyAndBoundaries` |
| Summary/compaction requests do not overwrite; complete replies and terminal states publish; partial replies excluded | Passed | `TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Actual model-pool member identity | Passed | `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Tool pairing, 20-exchange replay, budget trimming and overflow errors | Passed | `TestBuildRequestCompactionToolPairingAndBudget` |
| Main/side concurrency and bidirectional cancellation isolation | Passed | Blocking provider，`TestMainSideConcurrencyAndIndependentCancellation` |
| No tool execution, streaming/non-streaming, available usage on failure | Passed | `TestServiceNoToolsAndUsageOnFailure` |
| Real norma ChatAgent + local Read tool; main transcript/activity isolation | Passed | `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`, streaming and non-streaming subtests |
| Persistence, pagination, idempotency, partial answers after restart | Passed | `TestSideHistoryIdempotencyPagingAndRecovery` |
| Clear/late-write races, parent deletion, version comparison | Passed | `TestSideClearLateWritersAndDeletedParent` |
| MainAgent/Worker archive and restore, v1/v2/v3 | Passed | `TestSideTaskArchiveVersions` |
| Three parent APIs, authentication, ownership, logical Worker deletion | Passed | `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Side questions during a busy main session; independent SSE reconnect/disconnect, cancel and clear | Passed | `TestSideHTTPBusyIsolationClearAndReconnect` |
| One concurrent request per parent / four globally | Passed | Two `TestSideHTTP…` tests |
| Snapshot saved before admission, follow-up after restart, no fabricated old-session snapshots | Passed | `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Reject deleted cached profiles or changed models | Passed | `TestSideRejectsDeletedOrChangedCachedProfile` |
| Cancel and wait for final answer/usage persistence before archive | Passed | `TestSideTaskDrainPersistsBeforeArchive` |
| Record usage and side attribution once on early streaming-consumer cancellation | Passed | `TestSideUsageRecordedOnceOnConsumerCancellation` |
| Restart-restored Worker/deadline runtime contexts publish new snapshots | Passed | `TestSideRestoredWorkerRuntimePublishesNewCheckpoint` |
| Race checks for related packages | Passed | Commands below |
| TypeScript and production build | Passed | `npx tsc --noEmit`, `npm run build` |
| Biome for new frontend modules | Passed | `biome check`, three new modules |

Reproduce automated checks with `ARTEX_PG_DSN` set to a separate disposable database, never production:

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

The full Go regression was not green. Two existing `server` tests failed during temporary-directory cleanup with `TempDir RemoveAll … directory not empty`:

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

Exporting the unchanged baseline and rerunning `server` in the same isolated environment reproduced both cleanup failures. The baseline also failed the target-node count assertion in `TestCoreTaskLifecyclePG`; the final modified server regression did not. Other packages, side-question cases and race checks passed. Baseline issues were not counted as accepted, and existing assertions were not changed to hide them.

Next.js emitted the existing multiple-lockfile/workspace-root inference warning; the build and all page generation completed.

## Browser checks

Codex In-app Browser connected to isolated local Go and Next.js development servers. Desktop and 390 × 844 narrow-screen checks used browser automation and screenshot/log review:

- During ordinary chat, `/btw` displayed main and side content together in the desktop sidebar.
- Follow-ups worked; stopping retained partial side output while the main flow continued.
- Requests continued with the panel closed; reopening restored completed answers, and empty `/btw` restored history after refresh.
- Narrow-screen Drawer input, buttons, history and closing worked without horizontal overflow.
- Clear required confirmation, removed history and retained the main transcript/snapshot.
- Switching questions among MainAgent and two Workers kept labels and histories separate.
- A blocking local model fixture kept the Worker running. After submitting `/btw` from its main input and stopping the side request, the Worker still showed live execution and its own pause button, while partial side answers persisted.
- Browser error/warning logs were empty.

Controlled fixtures tested exact concurrency timing independently of model speed. Two initial Worker checks lacked a valid concurrency window because the task/answer had already ended. After fixing the fixture, repeated checks passed; those initial attempts were not counted as passes.

## Real-model conversations

The preferred `grok-4.6` probe at the OpenAI-compatible `http://127.0.0.1:12580/tingly/openai` returned HTTP 200, model `grok-4.6`, and `READY` in 2.82 seconds. Since it was available, neither Tingly `glm` nor Zhipu `glm-5.3` fallback was used or verified.

| Scenario | Actual result |
| --- | --- |
| Ask asset, objective and marker during main execution | Returned `redhaze.top`, homepage reading/summarization objective, `BTW-REAL-0910`; completed in 16.97 seconds |
| Ask tool evidence after homepage reading | Correct WebFetch 200, curl 301 → 302 → 200 redirects and title; 7.24 seconds |
| Ask side request to create a file with Bash | Refused; target file not created; 7.74 seconds |
| Completed side request leaves main context unchanged | Main transcript SHA-256 and activity unchanged; zero side tool executions |
| Follow up after actually stopping/restarting Go | Retained three prior side records; answered asset, marker and title from persisted snapshot without rerunning main agent |
| New conversation with non-streaming Grok | Correct asset and `ATOMIC-0910`; returned/saved usage: input 11734, output 138, cache_read 11520 |

The main conversation used WebFetch and Bash/curl to read the public homepage, landing at `https://id.redhaze.top/home`, titled “RedHaze Technology RedHaze Group · Global integrated group portal” (translated). Bash saved a local temporary response file; there was no remote write. This was verified separately from the absence of side-question tool execution.

Main transcript checksum: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

Usage limitation: Tingly Grok streaming responses returned no usage. A direct `stream_options.include_usage=true` probe returned HTTP 200, 12 data frames and zero usage frames. Streaming zeros therefore mean the endpoint supplied no usage, not no billing. Non-streaming usage and fixture failure/cancellation usage persisted correctly.

## Qwen review

Review model `qwen-flash` at OpenAI-compatible `https://dashscope.aliyuncs.com/compatible-mode/v1` returned HTTP 200. It received the first three real side conversations, main tool evidence and engineering assertions, and returned `verdict: accept`, `concerns: []`. It judged answers consistent with asset, marker and page evidence, and tool refusal consistent with constraints. Usage: prompt 6625, completion 312, total 6937.

The review excluded later restart and non-streaming tests. Qwen described “no writes” too broadly: main-session curl did create a local response file, as recorded above. Engineering assertions establish concurrency, zero side tool execution and transcript isolation; model review only assists answer-quality assessment.
