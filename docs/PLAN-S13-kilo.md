PLAN S13

# PLAN-S13 — S13 Multimodal (flagovi `vision`/`image-out`/`voice`/`video`/`documents`)

Jezgra `CGO_ENABLED=0`. S13 je pretežno ADAPTER (backend iza sučelja) + MEHANIZAM jezgra
(input MIME/size/duration caps + egress/consent + fail-closed heavy gate). Svaki flag je NEZAVISAN
(composable) — `voice` ne sili `video`. Mediji = UNTRUSTED input (P0.1); audio/video = CONFIDENTIAL
sensitivity (6.7 retention).

---

## 13.1 Vision input (screenshotovi, dijagrami) — `vision` flag

**Tip:** ADAPTER + MEHANIZAM (caps/egress)

**Aspekti → najbolji izvor:**
- screenshot+DOM hibrid → **browser-use**
- vision za computer use → **Open Interpreter**
- vision u browsing petlji → **OpenHands**

**Sinteza (Vision sučelje + backend + MIME/size caps + egress):**
```go
type Vision interface{ Describe(ctx, img Image) (string, error) }
type VisionRegistry struct{ byModel map[string]Vision } // backend po modelu (2.1 provider)
// MEHANIZAM jezgra: MIME allowlist + max_bytes caps (odbij prije decodea — DoS vektor)
// + egress/consent (6.3) za fetch slike + datamark UNTRUSTED (P0.1) — slika nije instrukcija
```

**Salvage:** NEXUSv2 `core/vision.py` (:71) → **Python→Go port**.

**Ugovor/RED:** 13.1 spec (caps/egress). RED (naš): slika preko max_bytes → odbij (prije decodea);
fetch slike na metadata-IP → egress REFUSE.

**Verifikacija:** browser-use (MIT), Open Interpreter (MIT), OpenHands (MIT) — stvarni.

**Pareto floor:** screenshot+DOM (browser-use) + computer-use (Open Interpreter) + browsing-petlja
(OpenHands) + MIME/size caps + egress (naš) → ≥ svaki.

---

## 13.2 Generacija/edit slika + canvas — `image-out` flag

**Tip:** ADAPTER + MEHANIZAM (canvas)

**Aspekti → najbolji izvor:**
- node-based pipeline, standard → **ComfyUI**
- produkcijski UI + API → **InvokeAI**
- programatska baza → **diffusers** (komponenta)

**Sinteza (ImageGen sučelje + canvas — strukturirani dijagram-out, ne injection-proof-tvrdnja):**
```go
type ImageGen interface{ Generate(ctx, prompt string) (Image, error) } // ComfyUI/InvokeAI/diffusers adapteri

// canvas (MEHANIZAM, NEXUS canvas.py): kompaktni validirani JSON scene/graph → deterministički render
type Canvas struct{}
func (c *Canvas) Render(scene json.RawMessage) (svg, png []byte, error)
// schema-validiran JSON + SANDBOXIRAN render + SVG sanitizacija OBAVEZNA
// (13.2: valjan JSON NE dokazuje odsutnost semantičke injekcije u labelama/linkovima)
// hipoteza (matrica): manji token-trošak od sirovog SVG-a — MJERI, ne tvrdi
```
Render = sandboxirani plugin (6.2); `image-out` flag; ComfyUI/InvokeAI/diffusers = ADAPTER, canvas = naš kod.

**Salvage:** NEXUSv2 `core/imagegen.py` (:266) + `core/canvas.py` + `core/image.py` +
`core/heavygen.py` (:162) → **Python→Go port**.

**Ugovor/RED:** 13.2 spec (schema-validiran + sandboxiran + sanitizacija). RED (naš): canvas JSON s
injection u labeli/linku → SVG sanitizacija uklanja (NE "injection-proof" tvrdnja — stvarna sanitizacija);
invalid JSON scene → schema-reject prije rendera.

**Verifikacija:** ComfyUI (GPL), InvokeAI (MIT), diffusers (Apache-2.0), Excalidraw/Mermaid (MIT) —
stvarni.

**Pareto floor:** node-pipeline (ComfyUI) + UI/API (InvokeAI) + baza (diffusers) + **canvas
dijagram-out sa sanitizacijom** (naš) → ≥ svaki.

---

## 13.3 Audio/voice (STT+TTS petlja) — `voice` flag

**Tip:** ADAPTER + MEHANIZAM (consent/retention)

**Aspekti → najbolji izvor:**
- realtime voice agent framework → **pipecat**
- produkcijski voice stack → **LiveKit Agents**
- kompatibilan STT/TTS server (whisper) → **speaches**

**Sinteza (Transcriber/Synthesizer sučelja + consent/retention jezgra):**
```go
type Transcriber interface{ Transcribe(ctx, audio Audio) (string, error) }
type Synthesizer interface{ Synthesize(ctx, text string) (Audio, error) }
// adapteri: pipecat (realtime) / LiveKit (produkcija) / speaches (whisper server)
// MEHANIZAM jezgra: duration/size caps; audio input = CONFIDENTIAL (P0.1 sensitivity)
//   → retention/delete kroz 6.7 (voice ne smije trajno ležati u indeksu bez politike)
// consent: snimanje mikrofona = eksplicitni consent gate (6.1)
```

**Salvage:** NEXUSv2 `core/voice.py` (transcriber :78, synthesizer :136) → **Python→Go port**.

**Ugovor/RED:** 13.3 spec (consent/retention). RED (naš): audio preko duration cap → odbij; voice
snimka bez consent-a → gate odbija; retention politika (6.7) primijenjena na audio.

**Verifikacija:** pipecat (BSD-2), LiveKit Agents (Apache-2.0), speaches (MIT) — stvarni.

**Pareto floor:** realtime (pipecat) + produkcija (LiveKit) + kompatibilan server (speaches) +
consent/retention (naš) → ≥ svaki.

---

## 13.4 Video obrada — `video` flag

**Tip:** ADAPTER + MEHANIZAM (caps)

**Aspekti → najbolji izvor:**
- deterministička obrada → **FFmpeg** (komponenta)
- word-level transkripcija + diarizacija → **WhisperX**
- generacija/video nodeovi → **ComfyUI**
- HTML/React → MP4 (bez GPU diffusera) → **Remotion/HyperFrames** (runner-up)

**Sinteza (Video sučelje info/understand + backend):**
```go
type Video interface {
    Info(ctx, v VideoRef) (Meta, error)            // duration/codec/resolution (FFmpeg probe)
    Understand(ctx, v VideoRef) (Transcript, error) // WhisperX transkripcija + diarizacija
}
// Remotion/HyperFrames = HTML/React → MP4 (agenti već znaju HTML — jeftinije od GPU diffusera)
// MEHANIZAM jezgra: duration/size caps (odbij prije obrade — DoS), egress za fetch
```

**Salvage:** NEXUSv2 `core/video.py` (info :196, understand :454) → **Python→Go port**.

**Ugovor/RED:** 13.4 spec (caps). RED (naš): video preko duration cap → odbij; transkripcija
datamarked UNTRUSTED (P0.1).

**Verifikacija:** FFmpeg (LGPL/GPL), WhisperX (BSD-2), ComfyUI (GPL), Remotion (source-available) —
stvarni.

**Pareto floor:** deterministička obrada (FFmpeg) + transkripcija+diarizacija (WhisperX) + video-node
(ComfyUI) + HTML→MP4 (Remotion) → ≥ svaki.

---

## 13.5 Sigurne operacije nad dokumentima — `documents` flag (Artifact-transformer)

**Tip:** MEHANIZAM (ugovor jezgra) + ADAPTER (format)

**Aspekti → najbolji izvor:**
- PDF manipulacija → **pypdf** (komponenta)
- OCR sloj → **OCRmyPDF** (komponenta)
- sign/verify → **pyHanko** (komponenta)

**Sinteza (INSTANCA generičkog Artifact-transformer ugovora — NE anatomija po formatu):**
```go
// GENERIČKI ugovor (V4 2:1) — isti za PDF/DOCX/spreadsheet/sliku:
type TypedOp struct{ Kind OpKind; Target string; Params json.RawMessage } // typed op, ne raw
type ArtifactTransformer interface {
    Preview(ctx, op TypedOp) (Preview, error)  // 1. preview/dry-run — vidi UČINAK prije primjene
    Apply(ctx, op TypedOp) (Result, error)     // 2. atomic staging (5.3) → output-verify → commit/rollback
}
// jezgra granice: path/signature/consent/atomic-write (5.3) + MIME/size caps + egress
// PDF adapter = pypdf/OCRmyPDF/pyHanko; NOVI format = NOVI adapter, NE nova podsekcija
// read-ingestion je ODRVOJEN u 10.1 (ovdje samo MUTACIJA)
```
NEXUS `pdf.py` adapteri ulaze TEK nakon parity testova (13.5 spec).

**Salvage:** NEXUSv2 `core/pdf.py` (87KB driver — adapteri) → **Python→Go port** (nakon parity testova).

**Ugovor/RED:** 5.3 (atomic write) + 13.5 (typed-op contract). RED (naš): dokument-op bez `Preview` →
odbijen (ne možeš mutirati bez uvida u učinak); atomic staging fail → rollback (5.3); op nad
nedopuštenim pathom → REFUSE (signature/consent).

**Verifikacija:** pypdf (BSD), OCRmyPDF (MPL-2.0), pyHanko (MIT) — stvarni.

**Pareto floor:** PDF manipulacija (pypdf) + OCR (OCRmyPDF) + sign/verify (pyHanko) + **generički
Artifact-transformer ugovor (preview→atomic→verify→commit)** (naš — nitko ne generalizira na "novi
format = adapter, ne podsekcija") → ≥ svaki.

---

## S13 cross-cutting napomene

1. **Composable flagovi:** svaki flag (`vision`/`image-out`/`voice`/`video`/`documents`) je NEZAVISAN
   — `voice` ne sili `video`; bundle-alias `multimodal` aktivira SAMO flagove čiji je okidač nastupio.
2. **ADAPTER vs MEHANIZAM:** backend (ComfyUI/pipecat/LiveKit/FFmpeg/WhisperX/pypdf) = ADAPTER iza
   sučelja; jezgra = MIME/size/duration caps + egress/consent + fail-closed heavy gate + canvas
   sanitizacija + Artifact-transformer ugovor.
3. **Mediji = UNTRUSTED + CONFIDENTIAL:** svi medijski inputi su `UNTRUSTED_EXTERNAL` (P0.1 — nikad
   instrukcija), audio/video = `CONFIDENTIAL` (6.7 retention/delete obvezno).
4. **13.5 je INSTANCA, ne anatomija:** Artifact-transformer ugovor (typed-op→preview→atomic→verify→
   commit/rollback) — novi format = adapter; NEXUS `pdf.py` tek nakon parity testova.
5. **Honest gap:** nijedna S13 podsekcija nema dedicirani Annex RED (gate = 5.3 + 6.3 + P0.1 posredno).
   Ako moderator želi simetriju: P0.17 "artifact-op-never-without-preview".
6. **Verifikacija:** svi kandidati stvarni (licence gore; ComfyUI GPL = izoliran adapter, ne linkan u
   jezgru); salvage `vision/imagegen/canvas/voice/video/pdf` interni (postoje — audit potvrdio), port je logika.
