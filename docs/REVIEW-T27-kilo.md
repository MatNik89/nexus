# T27 full-P0 adversarial review — Kilo

Target: `slice/p0-phase7` at HEAD `b604d43` (T27a live seal + delivery loop, T27b acceptance harness, T27c release signing, T27d doctor hardening). Final gate. Method: code-read every T27 change, run the suite in the repo, read the acceptance harness end-to-end. bwrap 0.11.2 present; the acceptance harness runs the real binary + bwrap.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`, including `internal/acceptance` (29.5s, all six criteria + sensitivity + hostile-under-yolo + release signature + doctor grant).

## What holds (verified, not rubber-stamped)

- **Black-box acceptance harness is genuine.** `internal/acceptance` builds the real `nexus` binary (`nexusBin`, `go build cmd/nexus`) and runs it as a subprocess (`world.daemon`), with a scripted provider + fake Bot API; the checker and worker are separate processes. The six MAIN criteria are non-vacuous: criterion 1 asserts a real model reply (`"echo: hello nexus"`), 2 asserts fact recall across a full restart (`"ACCEPTFACT is 41"`), 3 asserts the delivered message carries the exact occurrence id (`"occ-rem-accept#1"`) plus the ack, 4 asserts ASK→`APPROVAL NEEDED`→approve, 5 asserts the work fact is absent in private, 6 asserts `exit status: 0` from the sandboxed exec.
- **Hostile-under-yolo is real (F2 with the real backend).** `TestCriterion6HostileUnchangedUnderYolo` writes a `YOLO-CANARY` file outside the closure and asserts it stays unreadable, and that `/bin/sh -c echo PWNED` (un-promoted) is refused — both under `--yolo`.
- **Release signing is real.** `release-sign.sh`/`release-verify.sh` use `ssh-keygen -Y sign/verify` (namespace `nexus-release`, fail-closed verify); `TestReleaseSignatureTamperDetected` proves a flipped byte fails.
- **Doctor grant is mostly live.** `TestDoctorP0GrantLive` proves a fully-live world earns P0-capable (exit 0) and dropping the bot token turns `telegram` OFF and withdraws the grant (exit 1). Conversation (real provider round-trip), telegram (live getMe), and sandbox (trust-rooted canary) are genuinely measured.

## Findings

### 1. [MED] `doctor --p0` grants "reminders: LIVE" from the scheduler substrate, not the delivery loop

`runDoctorP0` measures the reminders criterion as `add("reminders", profilesOK, "durable scheduler substrate ready")` (`cmd/nexus/main.go:772-774`) — i.e. both profile journals open and no `scheduler_health` error. It never exercises the delivery loop the T27 change actually added: the owner-chat resolution (`main.go:179-191`), `EnqueueReply` (`:205`), `MarkDelivered` (`:210-211`), or the `ack` path.

Concrete failure: run `doctor --p0` with `default_profile="private"` and `NEXUS_TELEGRAM_BINDINGS="42=work"`. The telegram criterion passes (getMe probes the bot; the adapter is built for `private` regardless), the reminders criterion passes (journals open, no sweep error), so P0-capable is granted — but the delivery loop's `ownerChat` resolves to `""` (no chat bound to `private`), so reminders stay `DELIVERY_PENDING` forever with no consumer. The final gate's "P0-capable" declaration is earnable with a dead reminder-delivery capability — the one criterion that was "never live" before T27.

### 2. [LOW] `TestCriterion3SensitivityNoChannel` is vacuous

`internal/acceptance/acceptance_linux_test.go:294-324` never asserts the no-channel→no-delivery claim. Its only assertion is `if w.bot != nil { t.Fatal("test wiring error") }` — a test-wiring invariant that is true by construction (`withTelegram` was never called). The comment concedes "there is nothing to assert delivery on." The fail-closed behavior it should pin (no owner chat → reminder stays `DELIVERY_PENDING`, no fake receipt) is correct in the code but unproven; the sensitivity switch for criterion 3 does not actually turn the criterion RED.

### 3. [LOW] Reminder "delivery receipt" is the outbox ENQUEUE receipt, not the confirmed SEND

`MarkDelivered` records `ReceiptID: dlv` — the `EnqueueReply` outbox row id (`main.go:205-211`) — and the projection's `delivered_at` is the `EvDelivered` emitted time, both before the actual send (`FlushOutbox`). A reminder whose send stalls/fails is still DELIVERED and ackable. Within B2's at-least-once boundary (the enqueue row is durable and the send is at-least-once), but the "delivery receipt" proves enqueue, not delivery.

### 4. [LOW] `ownerChat` resolution is non-deterministic

`main.go:179-191` iterates the bindings map and returns the first chat whose profile matches; Go map order is random, so with multiple chats bound to the same profile the reminder targets a random chat.

### 5. [LOW] `telegram_api_base` is unvalidated and outside `egress_allow`

`internal/foundation/config/config.go` adds `telegram_api_base` as a plain string with no scheme/host validation, and the telegram egress (a builtin channel) is not covered by `egress_allow`. It is operator-controlled (config is the trust boundary, so not a true SSRF), but the bot token rides in the URL path (`/bot<token>/…`) to whatever endpoint is configured.

## Verdict

The core of T27 is real: the acceptance harness is genuinely black-box and its six criteria are non-vacuous, the hostile-under-yolo and release-signing REDs are real, and five of six doctor criteria are genuinely live. But the final gate has a material honesty gap — the "P0-capable" declaration is earnable with a dead reminder-delivery capability because the doctor's reminders criterion measures only the scheduler substrate, and the criterion-3 sensitivity switch is vacuous.

VERDICT: FAIL
