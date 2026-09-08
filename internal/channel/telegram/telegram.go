//go:build linux

// Package telegram is the built-in Telegram adapter (T23, channel:builtin
// — compiled in, attested by the release signature per P1.6 as amended;
// it does NOT pass the extensions closure). It long-polls getUpdates on
// the T22 durable core: per-chat profile binding is DENY-DEFAULT (an
// unbound chat receives a typed "which profile?" refusal and NOTHING is
// admitted — B3); non-text updates receive a typed fail-closed reply and
// the loop stays alive (C2); a replayed update_id returns the durable
// existing outcome, so redeliveries can never double-run the handler.
// Outbound replies ride the T22 outbox (at-least-once, honesty rules).
package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/closure"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

const adapterID = "telegram"

// Config seals the adapter's construction inputs. Bindings map chat id →
// profile slug; the adapter serves exactly ONE profile's journal, so a
// chat bound to a different profile is refused here (the composition
// runs one adapter per profile journal — B3 physical isolation).
type Config struct {
	APIBase  string
	TokenEnv string
	Bindings map[int64]string
	Profile  contracts.ProfileID
}

// Handler runs one admitted inbound message to a reply (the daemon wires
// the loop; tests wire fakes).
type Handler func(ctx context.Context, in channel.Inbound) (string, error)

// Adapter is the long-poll owner.
type Adapter struct {
	base     string
	token    string
	bindings map[int64]contracts.ProfileID
	profile  contracts.ProfileID
	core     *channel.Core
	handle   Handler
	client   *http.Client
	// offset is in-memory BY DESIGN (fresh-audit kilo F3): Telegram
	// confirms server-side via the NEXT getUpdates offset, so a restart
	// re-fetches only the unconfirmed tail; T22 dedup + stable refusal
	// ids keep redelivery idempotent.
	offset int64
}

func New(cfg Config, core *channel.Core, h Handler) (*Adapter, error) {
	if core == nil || h == nil {
		return nil, fmt.Errorf("telegram: core and handler are required (fail closed)")
	}
	if cfg.APIBase == "" || cfg.TokenEnv == "" || !cfg.Profile.Valid() {
		return nil, fmt.Errorf("telegram: api base, token env and profile are required (fail closed)")
	}
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return nil, fmt.Errorf("telegram: env var %s holds no bot token (fail closed)", cfg.TokenEnv)
	}
	b := make(map[int64]contracts.ProfileID, len(cfg.Bindings))
	for chat, p := range cfg.Bindings {
		pid := contracts.ProfileID(p)
		if !pid.Valid() {
			return nil, fmt.Errorf("telegram: invalid profile binding for chat %d (fail closed)", chat)
		}
		b[chat] = pid
	}
	return &Adapter{
		base: strings.TrimRight(cfg.APIBase, "/"), token: token,
		bindings: b, profile: cfg.Profile, core: core, handle: h,
		client: &http.Client{Timeout: 65 * time.Second},
	}, nil
}

// --- Bot API wire ---

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Text  string          `json:"text"`
		Photo json.RawMessage `json:"photo"`
		Voice json.RawMessage `json:"voice"`
		Doc   json.RawMessage `json:"document"`
	} `json:"message"`
}

// isPreWire reports whether a client.Do error happened before anything
// could reach the remote: dial-phase socket errors and DNS resolution
// failures. Everything else (reset/EOF/timeout after write) stays
// ambiguous — the remote may have accepted the request.
func isPreWire(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// sanitize strips the bot token from any error text (Phase-5 codex #9 /
// kilo #2 / agy #1: url.Error embeds the full bot<token> URL).
func (a *Adapter) sanitize(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), a.token, "[REDACTED-TOKEN]"))
}

func (a *Adapter) call(ctx context.Context, method string, req any, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.base+"/bot"+a.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		// Request construction can embed the full bot URL in parser
		// errors (Phase-5-r2 codex #8).
		return a.sanitize(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(httpReq)
	if err != nil {
		// Classify (Phase-5-r2 kilo #2): a dial/DNS-phase failure is
		// DEFINITE — nothing left the process, a retry is safe. Only a
		// failure after the request may have been sent is ambiguous
		// (Phase-5 codex #3). The token never leaks (codex #9).
		if isPreWire(err) {
			return fmt.Errorf("telegram: connect: %w", a.sanitize(err))
		}
		return fmt.Errorf("telegram: transport: %w: %w", channel.ErrAmbiguousSend, a.sanitize(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode >= 500 {
			// 5xx after a POST: the remote may have processed it.
			return fmt.Errorf("telegram: %s HTTP %d: %w", method, resp.StatusCode, channel.ErrAmbiguousSend)
		}
		return fmt.Errorf("telegram: %s HTTP %d", method, resp.StatusCode)
	}
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		// 2xx but unreadable body: the remote ACCEPTED — ambiguous for
		// effectful methods.
		return fmt.Errorf("telegram: malformed %s reply: %w: %w", method, channel.ErrAmbiguousSend, a.sanitize(err))
	}
	if !envelope.OK {
		return fmt.Errorf("telegram: %s not ok", method)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

// PollOnce fetches one getUpdates batch and processes every update. The
// offset advances ONLY past updates whose outcome is durable (admitted +
// handled, or typed-refused) — a crash before that redelivers, and the
// T22 dedup makes the redelivery return the existing outcome.
func (a *Adapter) PollOnce(ctx context.Context) error {
	var updates []tgUpdate
	if err := a.call(ctx, "getUpdates",
		map[string]any{"offset": a.offset, "timeout": 0}, &updates); err != nil {
		return err
	}
	for _, u := range updates {
		if err := a.processUpdate(ctx, u); err != nil {
			// The offset does NOT advance past a failed update: Telegram
			// redelivers it; T22 dedup keeps it exactly-once.
			return err
		}
		if u.UpdateID >= a.offset {
			a.offset = u.UpdateID + 1
		}
	}
	return nil
}

// refusalID derives the CALLER-STABLE delivery id for a typed refusal
// from the update identity (fresh-audit kilo F3): a crash between the
// refusal enqueue and the next poll's offset confirmation makes Telegram
// redeliver the update — the stable id turns the re-enqueue into an
// idempotent no-op instead of a second user-visible refusal.
func refusalID(identity string, updateID int64) string {
	sum := sha256.Sum256([]byte("tg-refusal|" + identity + "|" + strconv.FormatInt(updateID, 10)))
	return "dlv-" + hex.EncodeToString(sum[:12])
}

func (a *Adapter) processUpdate(ctx context.Context, u tgUpdate) error {
	if u.Message == nil {
		return nil // non-message update classes are ignored in P0
	}
	chat := u.Message.Chat.ID
	identity := "chat-" + strconv.FormatInt(chat, 10)
	// SINGLE-USER boundary (Phase-5 codex #5): only PRIVATE chats — in a
	// group every member would inherit the owner's USER trust. Fail
	// closed on anything else (and on a missing sender identity).
	if u.Message.Chat.Type != "private" || u.Message.From == nil {
		_, err := a.core.EnqueueReplyID(ctx, refusalID(identity, u.UpdateID), adapterID, identity, a.profile,
			"NEXUS talks only in a private chat with its owner.")
		return err
	}
	// DENY-DEFAULT profile binding (B3): an unbound chat — or one bound
	// to a DIFFERENT profile than this adapter serves — is refused with a
	// typed reply and NOTHING is admitted.
	bound, ok := a.bindings[chat]
	if !ok || bound != a.profile {
		_, err := a.core.EnqueueReplyID(ctx, refusalID(identity, u.UpdateID), adapterID, identity, a.profile,
			"This chat is not bound to a profile. Ask the NEXUS owner to bind it before I can talk here.")
		return err
	}
	// C2: only text is processable in P0 — typed fail-closed reply,
	// nothing admitted, loop alive.
	if u.Message.Text == "" {
		_, err := a.core.EnqueueReplyID(ctx, refusalID(identity, u.UpdateID), adapterID, identity, a.profile,
			"I can handle only text messages for now (photos, voice and files are not supported yet).")
		return err
	}
	in := channel.Inbound{
		AdapterID: adapterID, ChannelIdentity: identity,
		UpdateID: u.UpdateID, Text: u.Message.Text, Profile: a.profile,
	}
	outcome, err := a.core.Admit(ctx, in)
	if err != nil {
		return err
	}
	if outcome.Replayed {
		// Crash recovery (Phase-5 codex #2): an admitted-but-NON-TERMINAL
		// message was interrupted before its handler finished — RE-RUN it
		// (at-least-once handling over exactly-once admission). A
		// TERMINAL replay is done: skip.
		st, serr := a.core.InboundStatus(ctx, outcome.MessageID)
		if serr != nil {
			return serr
		}
		if st == channel.StateTerminal {
			return nil
		}
	}
	// Typing indicator while the turn runs (best-effort — a failure
	// here never affects delivery). Telegram shows it for ~5s.
	a.call(ctx, "sendChatAction", map[string]any{"chat_id": chat, "action": "typing"}, nil)
	reply, herr := a.handle(ctx, in)
	if herr != nil {
		// The admission stays durable; the failure gets a typed reply and
		// the message closes TERMINAL with it (one recipe batch).
		_, err := a.core.CompleteInbound(ctx, outcome.MessageID, adapterID, identity, a.profile,
			"I could not process that message. Try again, or check the daemon log.")
		return err
	}
	if reply == "" {
		return a.core.MarkInboundTerminal(ctx, outcome.MessageID)
	}
	// THE terminal-result+outbox recipe: terminal + reply in one batch.
	_, err = a.core.CompleteInbound(ctx, outcome.MessageID, adapterID, identity, a.profile, reply)
	return err
}

// FlushOutbox delivers pending outbox rows through sendMessage (the T22
// honesty rules own the marks).
func (a *Adapter) FlushOutbox(ctx context.Context) error {
	return a.core.Flush(ctx, func(o Outbound) error {
		chat, err := strconv.ParseInt(strings.TrimPrefix(o.ChannelIdentity, "chat-"), 10, 64)
		if err != nil {
			return fmt.Errorf("telegram: malformed channel identity %q", o.ChannelIdentity)
		}
		// FIRST-LEASE-ONLY FORMATTING (tgout plan): the rendered HTML
		// body is carried only when this delivery has never been
		// wire-attempted (attempts folds the pre-wire outbound_unknown
		// lease, so a pre-wire failure also consumes it — declared
		// degradation); every re-flush carries the ORIGINAL with no
		// parse_mode, so a Telegram parse rejection self-heals on the
		// next regular flush tick. This slice adds NO attempt and
		// changes no scheduling — only the carried bytes.
		if o.Attempts == 0 {
			// A pipe table renders NATIVELY via sendRichMessage (Bot
			// API 10.1, verified official) — the owner's live table
			// complaint. ONE wire send this tick: if it fails (a
			// pre-10.1 client 400), the row re-pends and the next tick
			// (attempts>0) carries plain — same first-lease discipline
			// as the HTML path, no double-send.
			// Only when the WHOLE reply fits the sendRichMessage limit —
			// never clip (a clipped body that earns SENT would silently
			// drop the suffix, codex #1). An over-limit table falls to
			// today's path; multipart is its own deferred slice.
			if hasPipeTable(o.Text) && fitsRich(o.Text) {
				return a.call(ctx, "sendRichMessage", map[string]any{
					"chat_id": chat, "rich_message": map[string]any{"markdown": o.Text}}, nil)
			}
			if rendered, ok := renderHTML(o.Text); ok {
				return a.call(ctx, "sendMessage", map[string]any{
					"chat_id": chat, "text": rendered, "parse_mode": "HTML"}, nil)
			}
		}
		return a.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": o.Text}, nil)
	})
}

// hasPipeTable reports whether the text contains a GFM pipe table
// (a header row followed by a |---| delimiter). Reuses render.go's
// table detection so both paths agree.
func hasPipeTable(text string) bool {
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if strings.Contains(lines[i], "|") && isTableDivider(lines[i+1]) {
			return true
		}
	}
	return false
}

// fitsRich reports whether the WHOLE text is within the
// sendRichMessage character limit (no clipping — lossless or not at
// all).
func fitsRich(s string) bool {
	const max = 32768
	return len([]rune(s)) <= max
}

// Outbound aliases the core row (keeps the Flush signature readable).
type Outbound = channel.Outbound

// Run drives poll+flush until ctx ends.
func (a *Adapter) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	a.registerCommands(ctx) // best-effort: "/" offers the command menu
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.PollOnce(ctx) // errors: the next tick retries; dedup holds
			a.FlushOutbox(ctx)
		}
	}
}

// registerCommands publishes the bot command menu (setMyCommands) so
// typing "/" in the chat offers the options. Best-effort: a failure is
// logged by the caller's next tick, never fatal.
func (a *Adapter) registerCommands(ctx context.Context) {
	cmds := []map[string]string{
		{"command": "help", "description": "Što NEXUS zna i popis komandi"},
		{"command": "pending", "description": "Čekaju li odobrenja"},
		{"command": "outbox", "description": "Poruke s neizvjesnom isporukom"},
		{"command": "approve", "description": "Odobri zahtjev (approve ch-...)"},
		{"command": "deny", "description": "Odbij zahtjev (deny ch-...)"},
		{"command": "retry", "description": "Ponovi odobrenje (retry ch-...)"},
		{"command": "ack", "description": "Potvrdi podsjetnik (ack occ-...)"},
		{"command": "redeliver", "description": "Ponovno pošalji poruku (redeliver dlv-...)"},
	}
	a.call(ctx, "setMyCommands", map[string]any{"commands": cmds}, nil)
}

// Probe is the LIVE channel probe for the T11 snapshot: one getMe round
// trip bound to the resolved config's hash.
func (a *Adapter) Probe(ctx context.Context, resolved config.Resolved) closure.ProbeResult {
	pr := closure.ProbeResult{Name: "channel", ConfigHash: resolved.ConfigHash()}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var me struct {
		IsBot bool `json:"is_bot"`
	}
	if err := a.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		pr.Detail = err.Error()
		return pr
	}
	if !me.IsBot {
		pr.Detail = "token does not identify a bot"
		return pr
	}
	pr.Passed = true
	return pr
}
