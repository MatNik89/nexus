# DESIGN — D1 AST symedit + D2 crypto-fix (greenfield Go, CGO_ENABLED=0)

Zatvara dvije otvorene rupe iz DESIGN-STATUS.md. Moderator (claude) vlastiti dizajn.
Pravilo: buildable Go, invarijante, RED imena. Bez proze.

---

# D1 — AST/LSP symedit (`pkg/edit/symedit`)

**Načelo:** symedit mijenja SIMBOL po njegovom AST/type identitetu, ne po tekstu — ne dira
istoimeni string/komentar/nepovezan simbol u drugom scope-u. Dvoznačno/nerazriješeno → REFUSE.
NIKAD tihi text-fallback (to je 4.3 exact/fuzzy ladder, odvojeno).

## Backend izbor (pure-Go, bez cgo)

```go
type SymBackend uint8
const (
    BackendGoNative SymBackend = iota + 1 // go/packages + go/types — EXACT, type-aware, v1 primarni (dogfood)
    BackendLSP                            // vanjski language server (subprocess) — textDocument/rename, ostali jezici
)
// NEMA tree-sitter tier za EDIT: sintaktički scope je heuristika (ne razrješava type-based reference
// preko datoteka); tree-sitter ostaje za 4.4 pretragu/repomap, NE za symedit.
```

- **Go:** `golang.org/x/tools/go/packages` (LoadAllSyntax) + `go/types` — pune type-informacije,
  package-wide reference resolucija. Pure Go, bez cgo. Ovo je mehanizam `gorename`/`gopls`.
- **Ostali jezici:** LSP klijent (S4.6 transport) → `textDocument/rename` na instaliran server
  (rust-analyzer/pyright/tsserver). Server = vanjski ADAPTER (capability-confirm pri gradnji);
  nema servera → `ErrSymeditUnsupported`, NE text-fallback.

## Tipovi

```go
type SymKind uint8
const ( SymFunc SymKind = iota+1; SymMethod; SymType; SymVar; SymConst; SymField; SymPackage )

// Cilj se identificira POZICIJOM (line,col — nedvosmisleno) ILI kvalificiranim imenom.
type SymTarget struct {
    FilePath string
    Line, Col int          // 1-indeksirano; primarni identitet (kao LSP)
    Qualified Optional[string] // "pkg.Type.Method" — fallback, mora razriješiti na TOČNO 1 objekt
}

type SymRenameReq struct {
    Target   SymTarget
    NewName  string        // mora biti validan identifier ciljnog jezika
    Backend  SymBackend
}

type FileEdit struct {
    FilePath     string
    PreimageHash [32]byte  // 5.3: konkurentni write → CONFLICT
    Edits        []TextEdit // ne-preklapajući, sortirani reverse-offset (apply bez re-indeksiranja)
}
type TextEdit struct{ StartByte, EndByte int; NewText string }

// Plan je PREVIEW (ne mutira) — ide u 5.3 atomic staging → verify(3.5) → commit.
type SymEditPlan struct {
    Symbol      ResolvedSymbol
    RefsTotal   int
    Files       []FileEdit
    Backend     SymBackend
    Unresolved  []string   // reference koje backend nije mogao potvrditi (ako >0 → REFUSE)
}
type ResolvedSymbol struct{ Kind SymKind; Name, Package string; DeclPos string }
```

## Sučelje + invarijante

```go
type SymEditor interface {
    Backend() SymBackend
    // Plan: NE mutira disk; vraća sve edite ILI grešku (fail-closed, nema parcijalnog rezultata).
    PlanRename(ctx context.Context, req SymRenameReq) (SymEditPlan, error)
}

var (
    ErrSymAmbiguous      = errors.New("target razriješen na >1 objekt ili 0 — REFUSE")
    ErrSymNameCollision  = errors.New("NewName sudara s postojećim bindingom u istom scope-u")
    ErrSymUnresolvedRefs = errors.New("backend ne može potvrditi sve reference (build-broken?) — REFUSE")
    ErrSymeditUnsupported = errors.New("nema type-aware backenda za jezik — nema text-fallbacka")
    ErrSymInvalidNewName  = errors.New("NewName nije validan identifier ciljnog jezika")
)
```

**Invarijante (obavezne):**
1. **Type-identitet, ne tekst:** preimenuje se SAMO identifier-token koji `go/types` (ili LSP) razriješi
   na ISTI `types.Object`. String-literal/komentar/istoimeni simbol u drugom scope-u = NETAKNUTI.
2. **Fail-closed na nesigurnost:** file se ne type-checka (build greška) ILI ima nerazriješenih
   referenci → `ErrSymUnresolvedRefs`, NULA edita. Ne pogađa se.
3. **Refuse-on-ambiguous:** Qualified target koji da ≠1 objekt → `ErrSymAmbiguous`.
4. **Collision-check:** `NewName` već vezan u dosegu bilo koje reference → `ErrSymNameCollision`
   (ne stvara shadowing/build-break tiho).
5. **Nema parcijalnog edita:** Plan je sve-ili-ništa; `Unresolved>0` → REFUSE (ne djelomičan rename).
6. **Preview-only:** PlanRename ne piše; primjena ide kroz 5.3 AtomicWriter + first-writer-wins.
7. **Cross-file potpunost (Go-native):** package-wide reference — sve datoteke paketa u planu, ne samo ciljna.

## RED

- `TestSymRenameSkipsStringAndComment` — funkcija `Foo` + string `"Foo"` + `// Foo` → rename `Foo→Bar`
  dira SAMO deklaraciju+pozive, string i komentar OSTAJU `Foo`.
- `TestSymRenameShadowedVarNotRenamed` — vanjski `x` i unutarnji `x` (drugi scope); rename vanjskog ne dira unutarnji.
- `TestSymRenameCrossFileRefs` — simbol korišten u 3 datoteke paketa → sve 3 u planu.
- `TestSymRenameCollisionRefused` — `NewName` već postoji u scope-u reference → `ErrSymNameCollision`, 0 edita.
- `TestSymRenameBuildBrokenRefused` — datoteka ne kompajlira → `ErrSymUnresolvedRefs`, 0 edita.
- `TestSymRenameAmbiguousQualifiedRefused` — `Qualified` da 2 objekta → `ErrSymAmbiguous`.
- `TestSymeditUnsupportedLangNoTextFallback` — jezik bez backenda → `ErrSymeditUnsupported` (NE tiho text-replace).
- `TestSymEditPlanIsPreviewOnly` — PlanRename ne mijenja nijedan bajt na disku.

---

# D2 — crypto-fix za sve "potpisane" artefakte (`pkg/attest`)

**Bug (agy):** `r.Signature = h.Sum(signerKey)` → `hash.Sum(b)` APPENDA digest na `b`, pa je
"potpis" = `signerKey || digest`: ključ CURI u output i nije keyed-MAC (krivotvorljivo). Isto bi
zarazilo shadow-checkpoint i audit-hash-chain (15.3). Zamjena:

## Signer sučelje + dvije implementacije

```go
type Signer interface {
    Sign(msg []byte) []byte
    Verify(msg, sig []byte) bool
    KeyID() string        // NIKAD ne otkriva tajni materijal
}

// (a) LOKALNI tamper-evidence (ExecutionReceipt, shadow-checkpoint): HMAC-SHA256, per-install ključ.
type HMACSigner struct{ key []byte; keyID string } // key iz OS keyringa (S6.4), NIKAD u artefaktu
func (s HMACSigner) Sign(msg []byte) []byte {
    m := hmac.New(sha256.New, s.key); m.Write(msg); return m.Sum(nil)
}
func (s HMACSigner) Verify(msg, sig []byte) bool {
    return hmac.Equal(sig, s.Sign(msg)) // constant-time
}

// (b) VANJSKI-anchor (15.3 audit-checkpoint, 6.8 supply-chain): ed25519 (javna verifikacija).
type Ed25519Signer struct{ priv ed25519.PrivateKey; pub ed25519.PublicKey; keyID string }
func (s Ed25519Signer) Sign(msg []byte) []byte { return ed25519.Sign(s.priv, msg) }
func (s Ed25519Signer) Verify(msg, sig []byte) bool { return ed25519.Verify(s.pub, msg, sig) }
```

## Kanonska serijalizacija PRIJE potpisa (deterministička)

```go
// Ad-hoc konkatenacija polja (agy) je krhka (redoslijed/kolizije). Kanonski bajtovi = jedini ulaz.
func canonicalReceiptBytes(r ExecutionReceipt) []byte
// length-prefixed, fiksni redoslijed polja; Signature=NIL tijekom računa; FSDelta sortiran po Path.

func SignReceipt(r *ExecutionReceipt, s Signer) {
    r.Signature = nil
    r.SignerKeyID = s.KeyID()
    r.Signature = s.Sign(canonicalReceiptBytes(*r))
}
func VerifyReceipt(r ExecutionReceipt, s Signer) bool {
    sig := r.Signature; r.Signature = nil
    return s.Verify(canonicalReceiptBytes(r), sig)
}
```

**Invarijante:**
1. Tajni materijal (HMAC key / ed25519 priv) NIKAD ne ulazi u serijalizirani artefakt (samo `KeyID`).
2. `Verify` koristi isti key (HMAC) ili javni key (ed25519); `hmac.Equal` = constant-time.
3. Potpis se računa nad KANONSKIM bajtovima s `Signature=nil`; bilo koja izmjena polja → Verify false.
4. Isti Signer ugovor za ExecutionReceipt, shadow-checkpoint i audit-chain (15.3) — jedan mehanizam.

## RED

- `TestReceiptTamperAnyFieldFailsVerify` — promijeni ExitCode/FSDelta/backend → `VerifyReceipt=false`.
- `TestReceiptSignerKeyNeverInSerialization` — kanonski bajtovi + artefakt ne sadrže `key` materijal.
- `TestHMACIsNotRawConcatenation` — `Sign(msg) != sha256(key||msg)` naivno; = HMAC konstrukcija.
- `TestHMACVerifyIsConstantTime` — Verify koristi `hmac.Equal` (ne `bytes.Equal`).
- `TestEd25519ExternalVerifyWithPublicKeyOnly` — verifikacija samo javnim ključem, bez privatnog.
- `TestAuditChainCheckpointSignedEd25519` — 15.3 checkpoint verifikabilan izvana (anti-full-rewrite).

---

## Placement
- D1 → `pkg/edit/symedit`; integrira 5.3 (staging/first-writer-wins) + 3.5 (verify prije commita) +
  S4.6 (LSP transport za ne-Go). Zamjenjuje agy `OpASTSymRename` stub.
- D2 → `pkg/attest`; zamjenjuje agy `GenerateReceipt` potpis + veže 15.3/6.8. Ključevi iz S6.4 keyring.
- OSTAJE OPEN: #3 stvarni-OS sandbox probe (tvoj hardver), #6 PRD (tvoja presuda).
