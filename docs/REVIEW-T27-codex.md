# T27 Full-P0 Adversarial Review — Codex

Target: `slice/p0-phase7` at `b604d4386e1dc81de7cdd54c77d693ea0f6bb1fe` (`3c9895a..b604d43`).

Method: source trace against the P0 owners, uncached repository suite, verbose black-box acceptance run from a clean `git archive HEAD` export, and targeted negative probes in separate clean exports. The working tree was not modified except for this requested report. No real Telegram credential or paid provider was contacted.

## Findings

1. [HIGH] The final capability snapshot is log-only; capabilities marked OFF still dispatch.

   Location: `cmd/nexus/main.go:238-265`, `cmd/nexus/main.go:536-568`, `cmd/nexus/main.go:897-921`.

   Failure scenario: the Telegram `getMe` probe passes, the provider rejects the startup round trip, and the provider later recovers. The adapter is started at line 243 before `liveProbes` and `Seal` run. The resulting snapshot marks `conversation` OFF and therefore `telegram` OFF, but the snapshot is only iterated for logging; it is neither retained nor consulted by `Serve`, the planner factory, or the already-running adapter. An attacker-reachable Telegram update is consequently planned and answered through a capability that the sealed dependency graph says is OFF.

   Clean-export failing probe:

   ```text
   CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run '^TestReviewOffTelegramStillDispatches$' -v
   acceptance_linux_test.go:129: telegram dispatched although sealed dependency conversation was OFF:
   "echo: [USER chan-chat-42-1]\nremote message after failed provider probe\n"
   FAIL
   ```

   A second probe showed the local chat path likewise returned a successful model response after its startup provider probe failed.

   Fix: seal before starting any consumer, retain the snapshot, and make adapter start, planner construction, and tool exposure require the relevant `snap.On` result. Request all six capabilities so unavailable capabilities are logged as OFF with their preserved probe reason instead of being omitted.

2. [HIGH] Reminder delivery is marked from local outbox admission, not a remote delivery receipt; the two-append crash window can also duplicate notifications.

   Location: `cmd/nexus/main.go:200-213`, `internal/channel/channel.go:375-395`, `internal/channel/channel.go:435-484`, `internal/channel/telegram/telegram.go:270-279`, `internal/obligation/obligation.go:787-819`.

   Failure scenario: `EnqueueReply` returns a locally generated `dlv-*` identifier immediately after inserting a PENDING row. The reminder loop passes that identifier to `MarkDelivered` before `FlushOutbox` touches Telegram. If Telegram rejects every send with HTTP 500, the outbox correctly remains UNKNOWN, but the obligation is already durably `DELIVERED` with a fabricated `Producer: telegram` receipt. If the daemon instead crashes after enqueue and before `MarkDelivered`, the occurrence remains DELIVERY_PENDING; restart creates a second random outbox row, so both rows can later reach the owner.

   Clean-export failing probe with a Bot API that passes `getMe` but returns HTTP 500 for every `sendMessage`:

   ```text
   CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run '^TestReviewReminderNotDeliveredBeforeRemoteReceipt$' -v
   acceptance_linux_test.go:236: reminder claimed DELIVERED although Telegram accepted no send
   FAIL
   ```

   Fix: durably associate one stable delivery ID with the occurrence, and transition the obligation to DELIVERED only from the channel owner's proven SENT transition carrying the remote receipt. UNKNOWN must remain unreconciled, and restart must reuse the same occurrence delivery rather than enqueue another row.

3. [HIGH] `doctor --p0` grants `P0-capable` from prerequisites, not six live PRD criteria.

   Location: `cmd/nexus/main.go:713-775`, `cmd/nexus/main.go:802-815`, `internal/acceptance/acceptance_linux_test.go:711-742`.

   Failure scenario: the doctor creates/opens empty `work` and `private` journals and labels memory and profile isolation live without performing a memory write/restart/recall or a cross-profile negative read. It labels reminders live when the scheduler-health file is absent and journals open; it never schedules, restarts, delivers, receives a remote receipt, or acknowledges an occurrence. The acceptance test invokes the doctor on a fresh world before any daemon or criterion flow has run and only tests token removal.

   Causal clean-export probe: I disabled the entire production reminder consumer by changing only `if ownerChat != ""` to `if false && ownerChat != ""`.

   ```text
   CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run '^TestDoctorP0GrantLive$' -v
   --- PASS: TestDoctorP0GrantLive (6.00s)
   PASS

   CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run '^TestCriterion3ReminderDeliversAfterRestart$' -v
   acceptance_linux_test.go:285: no outbound message containing "water the plants"; sent=[]
   FAIL
   ```

   Thus a demonstrably dead PRD criterion still earns the final grant.

   Fix: have doctor verify an immutable acceptance attestation bound to the exact binary digest, resolved config hash, host probe identity, and six criterion results, or run equivalent external criterion checks. Journal-open and health-file-absent may remain prerequisite diagnostics but cannot mint `P0-capable`.

4. [MED] `telegram_api_base` bypasses the egress policy and can disclose the bot token to an arbitrary configured endpoint.

   Location: `internal/foundation/config/config.go:32-36`, `internal/foundation/config/config.go:304-326`, `internal/channel/telegram/telegram.go:62-85`, `internal/channel/telegram/telegram.go:130-152`.

   Attacker/prerequisite: control of a config layer or `NEXUS_CFG_TELEGRAM_API_BASE`. The new field accepts any non-empty string; `ValidateBounds` does not bind it to `egress_allow`, HTTPS, or loopback. The adapter uses an unrestricted `http.Client` and puts the secret in `/bot<token>/...`. This turns a configuration injection or unsafe override into SSRF and direct token disclosure, bypassing the policy-aware egress boundary.

   Clean-export failing probe used a loopback API base absent from `egress_allow` and recorded the request path:

   ```text
   CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run '^TestReviewTelegramOverrideCannotBypassEgressWithToken$' -v
   acceptance_linux_test.go:211: telegram_api_base outside egress_allow received the bot token in path
   "/bot123:accept-token/getMe"
   FAIL
   ```

   Counterevidence checked: errors sanitize the token, but that protects diagnostics only; it does not stop the configured remote peer from receiving it.

   Fix: route Telegram through the policy-aware egress dialer with redirect revalidation and require the endpoint host to be allowed. Permit cleartext only for an explicit, validated loopback development endpoint; production overrides require HTTPS.

5. [MED] The black-box harness uses predictable process-global binary paths, so concurrent revisions can replace the artifact after `sync.Once` binds it.

   Location: `internal/acceptance/acceptance_linux_test.go:29-49`, `internal/acceptance/acceptance_linux_test.go:634-651`.

   Failure scenario: every checkout and every concurrent `go test` process builds to `${TMPDIR}/nexus-acceptance-bin` and `${TMPDIR}/nexus-acceptance-helper`. After one process caches that pathname, another clean export can overwrite it with a different revision. The first harness then executes an artifact not built from the revision it grades, defeating the final gate's revision binding and potentially producing either a false PASS or false FAIL.

   Probe: two clean exports were built sequentially into the exact same isolated `TMPDIR` path; the second had a distinct version marker.

   ```text
   first=f45b25e81a008b47b6e1cb9ec0c040520adbe4ff43ab384e572f2ce8a3b6b8ee
   after=a13647d9d0b0558e56d5a487b7ac20b45130586a8f622fab865b01361d4baa17
   FAIL: cached acceptance path was replaced by another clean export
   ```

   Fix: allocate one private package-lifetime temporary directory (for example in `TestMain`), build both artifacts inside it, and clean it after the package finishes. Never share a fixed basename directly under `os.TempDir()`.

6. [LOW] `TestCriterion3SensitivityNoChannel` is a vacuous sensitivity detector.

   Location: `internal/acceptance/acceptance_linux_test.go:293-324`.

   Failure scenario: after four seconds the only assertion is that the harness never assigned `w.bot`; it observes neither the binary nor the obligation/outbox state. With the production delivery loop completely disabled, this test still passed in 6.59s. Under the same ablation the real criterion-3 test correctly turned RED after failing to observe delivery. The primary criterion test is therefore causal, but the separately advertised feature-off sensitivity check proves only its own setup and did not detect finding 2's false receipt.

   Fix: expose a black-box status/receipt query or independently inspect the durable projection and assert that the occurrence remains DELIVERY_PENDING with no delivered receipt when no channel is bound.

## Verified controls and rejected hypotheses

- The harness does build and execute `cmd/nexus` as a subprocess for daemon and chat flows. A pristine clean-export verbose run executed all 15 acceptance tests with zero skips and passed in 20.571s. Finding 5 limits concurrent revision integrity, not the ordinary single-run path.
- `TestReleaseSignatureTamperDetected` passed from a clean export. Neutralizing only `release-verify.sh` made it RED with `tampered binary passed signature verification`; the fixed `nexus-release` namespace and whole-binary coverage are causal.
- The real `/usr/bin/bwrap` on this host is root-owned mode `0755`. Both the normal sandbox criterion and hostile-under-yolo test ran without skips.
- The exact-occurrence text and ack route carry `occ-rem-accept#1`, and the obligation projection enforces DELIVERED before ACKED. The blocker is the invalid delivery evidence minted before the ack.
- Profile-bound reminder destination selection filters bindings by the daemon journal profile. No cross-profile delivery was reproduced in this path.
- The provider, channel, and sandbox probe implementations perform real calls/canaries. Finding 1 is that their sealed result has no enforcement consumer, not that the individual probes are static.

## Required command evidence

```text
CGO_ENABLED=0 go vet ./...
PASS

CGO_ENABLED=0 go test -count=1 ./...
PASS — all packages; internal/acceptance 28.982s,
internal/preflight/probe 40.716s, internal/sandbox 15.850s

CGO_ENABLED=0 go test -count=1 -v ./internal/acceptance
PASS — 15/15 tests executed, 0 skips, 20.571s (clean git-archive export)
```

Topknot assessment: the T27 additions are not lean at the trust boundary because `runDoctorP0` duplicates substantial construction logic yet still measures prerequisites rather than consuming the external acceptance result. Removing that self-certification path in favor of one revision-bound acceptance attestation would reduce both code and false-grant states. No new dependency is needed.

Weakest link: the exact SIGKILL window between reminder enqueue and `MarkDelivered` was established by the two independent durable appends and stable-state probes, not by a timed process-kill harness. The premature DELIVERED state was reproduced directly; the duplicate-send branch remains source-proven rather than scheduler-timing-proven.

Proof ceiling: external paid-provider and real Telegram-account behavior were not exercised; local protocol-faithful servers were used. This review does not assess hostile root or non-Linux platforms.

Skill update: none. This is the first-pass T27 review; the existing audit rules already require revision binding, same-boundary sensitivity, and external checker evidence.

VERDICT: FAIL
