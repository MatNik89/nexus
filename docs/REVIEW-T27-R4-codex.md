`git archive HEAD | tar -x -C /tmp/nexus-t27-r4-codex.bwgcxS` — PASS
`CGO_ENABLED=0 go test -count=1 -run TestDoctorP0GrantLive ./internal/acceptance` — PASS
`CGO_ENABLED=0 go test -count=1 -run TestSealedOffStartupRunsNoConsumers ./internal/acceptance` — PASS
`CGO_ENABLED=0 go test -count=1 ./...` (run in `/home/matej/HARNESS/nexus`) — PASS
Temporary copy-only change: `if false && hex.EncodeToString(sum[:]) != acceptanceSignerFingerprint` — PASS
`CGO_ENABLED=0 go test -count=1 -run TestDoctorP0GrantLive ./internal/acceptance` after the copy-only change — FAIL as expected (`replaced trust anchor granted (exit 0)`)
VERDICT: PASS
