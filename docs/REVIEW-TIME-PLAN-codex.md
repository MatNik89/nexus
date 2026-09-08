# PLAN-TIME design review — Codex

## Scope and evidence binding

- Review target: branch `slice/p0-time`, commit `9677f16ce0658e364a0f2b1889efeaf52182218b`, tree `54871f5ce58651106557a936c09e4fef576ea025`.
- The target branch resolves exactly to the reviewed commit. `docs/PLAN-TIME.md` is present in that tree and has SHA-256 `902053a30368b159283fb890970314c92ca6f5236230d1060fa1ceda87e557a0` both in `git show` and in the clean archive.
- Review source: clean `git archive` export at `/home/matej/HARNESS/nexus-review-time-9677f16`, on disk under `/home/matej/HARNESS`, not `/tmp`. The export is disposable and is verified removed after this report is written.
- Owner sources read: `docs/PRD.md`, `docs/ARCHITECTURE-ESSENTIALS.md`, the P0 contracts in `docs/HARNESS-SPEC.md`, `docs/tasks-P0.md`, the current planner and planner tests, and the canonical clock seam.
- Stolen-pattern source verified in the owner's local checkout: `/home/matej/.hermes/hermes-agent/agent/context_compressor.py:4512-4528`; the actual injected line begins at `/home/matej/.hermes/hermes-agent/agent/context_compressor.py:4519`. It is a guarded temporal anchor for the compression summarizer, not evidence that a live chat planner answers time questions correctly.
- Constraint: static design review only. No production implementation, production test, model call, or live bot probe was run.

## Findings

[CONCRETE][SEV: MEDIUM] `docs/PLAN-TIME.md:18` — the proposed package-global `nowFn` bypasses the repository's canonical clock owner and creates mutable process-global test state.

`internal/foundation/clockid/clockid.go:1-4` says every downstream component takes the shared clock interface and must not invent another clock source; `docs/tasks-P0.md:78-89` records that seam as the T05 owner. The planner already has concurrent-use potential, while a package variable can be replaced and restored by tests independently of planner instances. That permits cross-test contamination or a race and gives the time prompt a different clock owner from the rest of NEXUS. The smallest code is not the smallest correct design when it duplicates an existing owner.

PROBE: static owner trace from the plan to `ChatPlanner.Plan` (`internal/llm/planner/planner.go:235-330`), its constructors (`internal/llm/planner/planner.go:128-147`), and the canonical clock contract.

FIX: revise the plan so each planner receives the existing `clockid.Clock` (or a narrow instance-owned adapter derived from it), captures `Now()` exactly once per `Plan` call, and converts that captured instant to the resolved host location; do not mutate a package global.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:16-20` — the privileged system-prompt field has no closed grammar for the timezone label.

The numeric date/time and weekday are formatter-owned, but a zone abbreviation is data. Go's `time.FixedZone` accepts an arbitrary name (`/home/matej/.local/go/src/time/zoneinfo.go:110-138`), and loaded TZif abbreviations are copied into the location's zone name (`/home/matej/.local/go/src/time/zoneinfo_read.go:219-271`). The plan does not bound, validate, or encode that label before interpolation into the system role. Trace: local launcher or custom timezone data (and the proposed test seam) -> zone label -> formatted line -> `system` message -> provider (`internal/llm/planner/planner.go:246-263`). A Telegram message cannot control this source, and control of the service environment is already powerful, so this is not a demonstrated remote vulnerability; it is nevertheless an unresolved privileged-prompt grammar flaw.

PROBE: static source-to-sink trace plus counterevidence check: user blocks remain in the assembled user message and untrusted content remains fenced, but no equivalent constraint is specified for the zone label.

FIX: define a small allowed timezone-label grammar and length, with any empty or non-conforming label forcing the whole rendered time to UTC; add a hostile label case such as a newline plus an instruction and assert that none of it reaches the provider.

[CONCRETE][SEV: MEDIUM] `docs/PLAN-TIME.md:18-20` — "if the zone lookup fails" has no specified operation, error channel, or deterministic fallback detector.

`time.Now()` does not return a zone-resolution error. On Unix, Go's `time.Local` initialization silently falls back to UTC when local timezone loading fails (`/home/matej/.local/go/src/time/zoneinfo_unix.go:28-68`), while `Time.Zone()` only returns a name and offset (`/home/matej/.local/go/src/time/time.go:1401-1405`). The plan therefore cannot distinguish a successful UTC host configuration from failed local-zone discovery, and it does not say what happens for an empty/invalid abbreviation or a clock seam returning a non-host location. “UTC fallback” is an outcome assertion without an owned failure boundary.

PROBE: static API-contract inspection against Go 1.26.4, the toolchain present during review.

FIX: name the location resolver and its error contract separately from the clock, resolve the host location at an explicit lifecycle point, and specify fail-to-UTC behavior. Test successful host-zone conversion and forced resolution failure independently.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:38-40` — the cache rationale incorrectly calls the timestamp a changing tail and leaves its actual placement unspecified.

In the current planner, the system prompt is followed by the optional tool protocol and sorted descriptions (`internal/llm/planner/planner.go:246-259`), then history and the current user message (`internal/llm/planner/planner.go:260-263`). Appending immediately to `systemPrompt` would put the changing value before tool definitions; appending after tool definitions would make it only the tail of the system message, not of the complete request. DeepSeek's official context-cache contract requires a fully matching cached prefix unit and describes cache hits in terms of overlapping input prefixes: <https://api-docs.deepseek.com/guides/kv_cache>. Minute resolution limits churn but does not make the stated “changing tail” claim true.

PROBE: static message-order trace plus the provider's documented cache-hit rule.

FIX: specify that the time line is appended after all static/tool system content, state honestly that only the prefix before it can remain reusable at a minute rollover, and treat cache benefit as unproven until `prompt_cache_hit_tokens` is measured. Add an ordering assertion so a later implementation cannot place time before the tool list.

[CONCRETE][SEV: MEDIUM] `docs/PLAN-TIME.md:29-35` — the detector set proves only presence under one frozen clock and misses the plan's material semantics and attack boundary.

The stated exact-line checks can pass if time is captured once at planner construction rather than refreshed per call; they do not force the UTC fallback, hostile/empty zone handling, same-minute stability, minute rollover, one-clock-read consistency, or the streaming transport path. Keeping `TestToolPromptDeterministic` green checks the existing sorted tool IDs (`internal/llm/planner/planner_test.go:281-331`), but it is not a causal detector for cache placement or production-time refresh. Only “remove all injection” is named as an ablation; the fallback, sanitization, refresh, and ordering claims have no red-capable negative mutations.

PROBE: detector-to-requirement matrix against every claim in `docs/PLAN-TIME.md:13-40` and the three provider branches in `internal/llm/planner/planner.go:272-330`.

FIX: require committed detectors for (1) two calls across a minute boundary with one clock read per call, (2) two instants within one minute producing identical time bytes, (3) host-zone conversion plus forced UTC fallback, (4) hostile/empty zone labels, (5) exact placement after tool descriptions, and (6) buffered plain, streaming plain, and `WithTools` paths. Keep each detector installed while mutating only its claimed protection to establish RED.

## Decisions that hold

- The plan correctly puts the anchor in `ChatPlanner.Plan`, so a multi-step turn obtains a fresh anchor for each physical Plan call rather than freezing it for the daemon lifetime (`docs/PLAN-TIME.md:13-20`; `internal/kernel/loop/loop.go:275`).
- Minute resolution matches the requested answer granularity. It necessarily permits up to 59 seconds of quantization plus provider latency; that is an explicit proof ceiling, not a reason to add seconds.
- The no-tool non-goal is sound for this slice (`docs/PLAN-TIME.md:22-25`). Reading a process clock is not an external effect, and adding a model-callable tool would create schema, policy, and loop machinery without improving the stated minute-resolution outcome. A clock tool becomes justified only if a later requirement needs model-directed refresh or finer precision.
- The plan cites the Hermes line accurately, but only as a pattern source. Hermes guards a date line at `/home/matej/.hermes/hermes-agent/agent/context_compressor.py:4517-4528`; NEXUS's always-present clock and live-planner purpose require their own fallback and detector contract.

## Weakest link and proof ceiling

The weakest link is the absent location-resolution and output-grammar contract: it makes both UTC fallback and prompt-injection resistance impossible to verify causally. This review establishes design defects at the immutable plan revision only. It does not establish runtime behavior, answer quality, actual DeepSeek cache-hit rate, or correctness of any future implementation.

VERDICT: FAIL
