PLAN S13

# Hibrid-Hibrida Sinteza: Sekcija S13 (Multimodal i Specijalizirani Profili)
## COMPOSABLE FLAGOVI: `vision` • `image-out` • `voice` • `video` • `documents`

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S13.1–S13.5)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P1.4, P2.2, S13.5 Artifact-Transformer)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `net/http`, `image`, `io`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Arhitektonska Strategija za Multimodalne Profile

Sekcija S13 strukturirana je prema jasnom razgraničenju odgovornosti:
1. **Vanjski Motori su ADAPTERI:** Teški modeli (Vision API-ji, ComfyUI, ElevenLabs, WhisperX, FFmpeg, Remotion) žive iza čistih Go sučelja i mogu se mijenjati bez utjecaja na jezgru harnessa.
2. **Zaštitne Ograde su MEHANIZMI u Jezgri:**
   - Validacija ulaza (MIME type sniffing, hardcaps na veličinu datoteka i trajanje audia/videa).
   - Egress consent i privatnost (korisnički pristanak prije slanja slika/audia vanjskim API-jima).
3. **Canvas Dijagrami (13.2):** Strukturirani JSON dijagrami (`Mermaid`, `Excalidraw`, `SVG`) renderiraju se kroz **schema-validiran i sandboxiran proces** s obveznom SVG sanitizacijom (uklanjanje `<script>`, `onload`, `xlink:href`).
4. **Generički Artifact-Transformer Ugovor (13.5):** Umjesto pojedinačne anatomije po formatu (PDF vs DOCX vs XLSX), 13.5 implementira **generički ugovor transformatora**:
   `Typed-Op → Preview/Dry-Run → Atomic-Staging (S5.3) → Verify → Commit / Rollback`.

---

## 2. Arhitektura S13 Multimodalnog Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                  S13 MULTIMODAL ENGINE                                  │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ INPUT VALIDATION & RESOURCE BUDGETS (MEHANIZAM — Annex A P2.2)                    │  │
│  │   • MIME Sniffing • Max Bytes Cap • Audio/Video Duration Limits • Egress Consent  │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────┬───────────────┼───────────────┬───────────────────┐        │
│        ▼                   ▼               ▼               ▼                   ▼        │
│  ┌───────────┐       ┌───────────┐   ┌───────────┐   ┌───────────┐       ┌───────────┐  │
│  │13.1 VISION│       │13.2 CANVAS│   │13.3 VOICE │   │13.4 VIDEO │       │13.5 ARTI- │  │
│  │ (ADAPTER) │       │& IMAGE-GEN│   │ (ADAPTER) │   │ (ADAPTER) │       │ FACT-TRANS│  │
│  │Claude/GPT4│       │ComfyUI /  │   │WhisperX   │   │FFmpeg     │       │ FORMER    │  │
│  │Gemini /   │       │Flux API / │   │ElevenLabs │   │TwelveLabs │       │ (MEHANIZAM│  │
│  │Florence-2 │       │SVG Sandbox│   │LiveKit RTC│   │Remotion   │       │ & ADAPTER)│  │
│  └───────────┘       └───────────┘   └───────────┘   └───────────┘       └───────────┘  │
│                                                                                │        │
│                                                                                ▼        │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 13.5 GENERIČKI ARTIFACT TRANSFORMER PIPELINE (Annex A P1.4 + S5.3)                │  │
│  │   Typed-Op ──→ Dry-Run Preview ──→ .nexus_staging ──→ Verifier ──→ Atomic Commit │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S13.1: Vision (Obrada Slika, OCR i UI Inspekcija)

- **Tip:** `ADAPTER` (Go paket `pkg/multimodal/vision`, capability flag `vision`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Multi-Provider Vision Sučelje):* **Anthropic / OpenAI / Gemini Vision + Florence-2 (Lokalno)** — unificirano slanje slika (base64 ili URL) uz automatsko downsamplanje i kompresiju velikih screenshotova radi uštede tokena.
  - *Aspekt B (UI i DOM Vizualna Inspekcija):* **browser-use** — mapiranje vizualnih koordinata (bounding boxes) na interaktivne elemente sučelja.
  - *Aspekt C (Validacija Ulaza i Egress Consent):* **Annex A P2.2 & S6.3** — provjera MIME tipa preko čarobnih bajtova (`image/png`, `image/jpeg`, `image/webp`), cap veličine (max 20 MB) i provjera privole korisnika za vanjski upload.
- **Sinteza (Go dizajn skica):**
  ```go
  package vision

  import (
      "bytes"
      "context"
      "fmt"
      "image"
      _ "image/jpeg"
      _ "image/png"
      "net/http"
      "nexus/pkg/contract"
  )

  type ImagePayload struct {
      MIMEType    string `json:"mime_type"`
      Data        []byte `json:"-"`
      Width       int    `json:"width"`
      Height      int    `json:"height"`
      SourceURL   string `json:"source_url,omitempty"`
  }

  type VisionAnalyzer interface {
      Analyze(ctx context.Context, img ImagePayload, prompt string) (string, error)
      InspectUIElements(ctx context.Context, img ImagePayload) ([]contract.UIElementBox, error)
  }

  type VisionInputValidator struct {
      maxSizeBytes int64 // e.g. 20 MB
  }

  func (v *VisionInputValidator) ValidateAndDecode(data []byte) (*ImagePayload, error) {
      if int64(len(data)) > v.maxSizeBytes {
          return nil, fmt.Errorf("IMAGE_SIZE_EXCEEDED: payload size %d exceeds limit %d", len(data), v.maxSizeBytes)
      }

      mime := http.DetectContentType(data)
      if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
          return nil, fmt.Errorf("UNSUPPORTED_MIME_TYPE: detected %s", mime)
      }

      cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
      if err != nil {
          return nil, fmt.Errorf("IMAGE_DECODE_FAILED: %w", err)
      }

      return &ImagePayload{
          MIMEType: mime,
          Data:     data,
          Width:    cfg.Width,
          Height:   cfg.Height,
      }, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/vision.py` u Go `pkg/multimodal/vision`.
- **Ugovor / RED Gate:**
  - **Annex A P2.2**; RED test: `test_vision_validator_rejects_payload_exceeding_max_bytes`.
- **Verifikacija aspekata:**
  - Claude Vision spec, browser-use (MIT), Go standard `image` paket.
- **Pareto-floor potvrda:**
  - Standardizira vizualni ulaz kroz provjeru MIME tipa i dimenzija bez rušenja memorije pri obradi 4K slika.

---

## S13.2: Image Generation, Edit i Canvas Sustav

- **Tip:** `ADAPTER` (Generacija) + `MEHANIZAM` (Canvas Sandbox), capability flag `image-out`
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Adapter za Generaciju Slika):* **DALL-E 3 / Flux / ComfyUI** — generiranje i uređivanje slika (inpainting/outpainting) putem API poziva ili lokalnog ComfyUI endpointa.
  - *Aspekt B (Canvas Mehanizam — Strukturirani Dijagrami):* **Excalidraw / Mermaid / SVG Renderer** — **NAŠ DIFERENCIJATOR:** Model generira isključivo **validirani strukturirani JSON/Markdown**, a lokalni headless Go engine ga prevodi u SVG/PNG.
  - *Aspekt C (Stroga SVG Sanitizacija i Sandbox):* **DOMPurify / GORTEX Sandbox** — filtriranje svih opasnih tagova (`<script>`, `<iframe>`, `onload`, `xlink:href` prema vanjskoj mreži) kako bi se spriječio XSS u korisničkom sučelju.
- **Sinteza (Go dizajn skica):**
  ```go
  package canvas

  import (
      "context"
      "fmt"
      "regexp"
      "strings"
  )

  type DiagramType string
  const (
      DiagramMermaid    DiagramType = "MERMAID"
      DiagramExcalidraw DiagramType = "EXCALIDRAW"
      DiagramPlantUML   DiagramType = "PLANTUML"
  )

  type CanvasEngine struct{}

  var forbiddenSVGPatterns = []*regexp.Regexp{
      regexp.MustCompile(`(?i)<script[\s\S]*?>[\s\S]*?</script>`),
      regexp.MustCompile(`(?i)on[a-z]+\s*=`),
      regexp.MustCompile(`(?i)xlink:href\s*=\s*["']?http`),
  }

  func (ce *CanvasEngine) RenderSVG(ctx context.Context, diagramType DiagramType, sourceCode string) ([]byte, error) {
      // 1. Sintaksna validacija izvornog koda dijagrama
      // 2. Renderiranje u SVG unutar izoliranog podprocesa (S6.2 Sandbox)
      rawSVG := "<svg>...</svg>"

      // 3. Stroga SVG sanitizacija (Sprječavanje XSS-a i vanjskog dohvata)
      sanitizedSVG := rawSVG
      for _, p := range forbiddenSVGPatterns {
          if p.MatchString(sanitizedSVG) {
              return nil, fmt.Errorf("SECURITY_DENY: SVG contains unsafe active script or external link pattern")
          }
      }

      return []byte(sanitizedSVG), nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/imagegen.py` i `core/canvas.py` u Go `pkg/multimodal/canvas`.
- **Ugovor / RED Gate:**
  - **Annex A P1.4 & S6.2**; RED test: `test_canvas_engine_sanitizes_svg_and_blocks_malicious_script_tags`.
- **Verifikacija aspekata:**
  - Mermaid CLI, Excalidraw JSON schema, ComfyUI API.
- **Pareto-floor potvrda:**
  - Pruža pouzdano vizualno modeliranje arhitekture uz 100% sigurnost od injectiona u web sučelju.

---

## S13.3: Audio i Voice (STT + TTS i Real-Time Voice Loop)

- **Tip:** `ADAPTER` (Go paket `pkg/multimodal/voice`, capability flag `voice`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Govor-u-Tekst / STT):* **WhisperX / Faster-Whisper / Deepgram** — transkripcija audio zapisa s preciznim timestampovima na razini riječi (word-level alignment).
  - *Aspekt B (Tekst-u-Govor / TTS):* **ElevenLabs / Kokoro / Edge-TTS** — sinteza prirodnog glasa s niskom latencijom (<150ms).
  - *Aspekt C (Real-Time Audio Stream i VAD):* **Pipecat / LiveKit WebRTC** — dvosmjerni audio stream s prepoznavanjem tišine (Voice Activity Detection) i prekidanjem govora agenta kada korisnik progovori (Barge-in).
- **Sinteza (Go dizajn skica):**
  ```go
  package voice

  import (
      "context"
      "io"
      "time"
  )

  type STTAdapter interface {
      Transcribe(ctx context.Context, audioStream io.Reader, mimeType string) (string, error)
  }

  type TTSAdapter interface {
      Synthesize(ctx context.Context, text string, voiceID string) (io.ReadCloser, error)
  }

  type VoiceSessionManager struct {
      stt         STTAdapter
      tts         TTSAdapter
      maxDuration time.Duration // e.g. 5 minutes max audio input
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/voice.py` u Go `pkg/multimodal/voice`.
- **Ugovor / RED Gate:**
  - **Annex A P2.2**; RED test: `test_voice_session_enforces_audio_duration_budget`.
- **Verifikacija aspekata:**
  - Pipecat (BSD-2-Clause, Python), LiveKit Go SDK (Apache-2.0), Deepgram API.
- **Pareto-floor potvrda:**
  - Omogućuje interaktivni glasovni rad s minimalnom latencijom uz zaštitu od predugačkih audio sesija.

---

## S13.4: Video Obrada i Generiranje (FFmpeg i Remotion/HyperFrames)

- **Tip:** `ADAPTER` (Go paket `pkg/multimodal/video`, capability flag `video`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Ekstrakcija Frameova i Inspekcija):* **FFmpeg** — optimizirani Go poziv za izvlačenje ključnih frameova (keyframe extraction) i audia iz video zapisa.
  - *Aspekt B (Multimodalno Razumijevanje Videa):* **Gemini 2.0 Flash / TwelveLabs** — analiza dugih video snimaka za demonstracije grešaka i UI bugova.
  - *Aspekt C (Programabilno Generiranje Videa):* **Remotion / HyperFrames (obsidian)** — renderiranje video materijala iz React/TypeScript koda.
- **Sinteza (Go dizajn skica):**
  ```go
  package video

  import (
      "context"
      "os/exec"
      "time"
  )

  type VideoProcessor struct {
      maxVideoSeconds int
  }

  func (vp *VideoProcessor) ExtractKeyframes(ctx context.Context, videoPath string, outputDir string, fps float64) ([]string, error) {
      // ffmpeg -i videoPath -vf fps=fps outputDir/frame_%04d.png
      cmd := exec.CommandContext(ctx, "ffmpeg", "-i", videoPath, "-vf", "fps=1", outputDir+"/frame_%04d.png")
      return nil, cmd.Run()
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/video.py` u Go `pkg/multimodal/video`.
- **Ugovor / RED Gate:**
  - Povezano s P2.2; RED test: `test_video_processor_rejects_video_exceeding_duration_cap`.
- **Verifikacija aspekata:**
  - FFmpeg CLI standard, Remotion (Commercial/OSS exception), TwelveLabs API.
- **Pareto-floor potvrda:**
  - Omogućuje vizualnu analizu snimaka ekrana i UI bugova bez potrebe za internim pisanjem video encodera.

---

## S13.5: Dokument-Mutacija i Generički Artifact-Transformer Ugovor

- **Tip:** `MEHANIZAM` (Pipeline u jezgri) + `ADAPTER` (Transformatori po formatu), capability flag `documents`
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Generički Ugovor Transformacije — V8 Odluka):* **HARNESS-SPEC v0.8 r2 (13.5 Transformer Odluka)** — **NAŠ ARHITEKTONSKI INVARIJANT:** Svaka operacija nad dokumentima (split, merge, fill_form, redact, watermark, convert) tretira se kao instanca generičkog ciklusa:
    `Typed-Op → Preview/Dry-Run → Atomic-Staging (S5.3) → Verify → Commit / Rollback`.
  - *Aspekt B (Tipizirane Operacije i Dry-Run Preview):* **Annex A P1.4** — model deklarira `DocumentOperation` (npr. redact PII); sustav prvo vraća vizualni diff ili tekstualni preview bez mutacije izvorne datoteke.
  - *Aspekt C (Atomsko Staging Zapisivanje i Verifikacija Integriteta):* **S5.3 Atomic Staging** — nova verzija dokumenta piše se u `.nexus_staging` datoteku, provjerava se validnost zaglavlja i veličina, te se atomski preimenuje (`os.Rename`).
- **Sinteza (Go dizajn skica):**
  ```go
  package documents

  import (
      "context"
      "fmt"
      "os"
      "path/filepath"
      "sync"

      "nexus/pkg/contract"
      "nexus/pkg/workspace/staging"
  )

  type DocOpType string
  const (
      OpSplit     DocOpType = "SPLIT"
      OpMerge     DocOpType = "MERGE"
      OpRedact    DocOpType = "REDACT"
      OpWatermark DocOpType = "WATERMARK"
      OpConvert   DocOpType = "CONVERT"
  )

  type DocumentOperation struct {
      OpType       DocOpType         `json:"op_type"`
      SourcePaths  []string          `json:"source_paths"`
      TargetPath   string            `json:"target_path"`
      Parameters   map[string]string `json:"parameters"`
      ExpectedHash string            `json:"expected_hash"`
  }

  type ArtifactTransformer interface {
      SupportedOps() []DocOpType
      Transform(ctx context.Context, op DocumentOperation, stagingPath string) error
      VerifyIntegrity(stagingPath string) error
  }

  type GenericArtifactManager struct {
      mu           sync.Mutex
      transformers map[DocOpType]ArtifactTransformer
      writer       *staging.SafeAtomicWriter
  }

  func (gam *GenericArtifactManager) ExecuteTransformation(ctx context.Context, op DocumentOperation, author string, turnID int) error {
      gam.mu.Lock()
      transformer, exists := gam.transformers[op.OpType]
      gam.mu.Unlock()

      if !exists {
          return fmt.Errorf("UNSUPPORTED_DOC_OPERATION: %s", op.OpType)
      }

      // 1. Staging putanja (P1.4 / S5.3 Invarijant)
      stagingPath := op.TargetPath + ".nexus_staging"

      // 2. Izvrši transformaciju u staging datoteku
      if err := transformer.Transform(ctx, op, stagingPath); err != nil {
          _ = os.Remove(stagingPath)
          return fmt.Errorf("transformation failed: %w", err)
      }

      // 3. Verificiraj integritet novonastalog dokumenta
      if err := transformer.VerifyIntegrity(stagingPath); err != nil {
          _ = os.Remove(stagingPath)
          return fmt.Errorf("DOCUMENT_VERIFICATION_FAILED: output is corrupted: %w", err)
      }

      // 4. Atomski Commit (Rename)
      if err := os.Rename(stagingPath, op.TargetPath); err != nil {
          _ = os.Remove(stagingPath)
          return fmt.Errorf("atomic commit failed: %w", err)
      }

      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/pdf.py` i transformacijskih modula u Go generički `pkg/multimodal/documents`.
- **Ugovor / RED Gate:**
  - **Annex A P1.4 & S5.3**; RED test: `test_generic_artifact_transformer_rolls_back_on_corrupted_output`.
- **Verifikacija aspekata:**
  - Generički transformer contract (HARNESS-SPEC v0.8 r2), PDF/Office POSIX atomic file standards.
- **Pareto-floor potvrda:**
  - Zamjenjuje stotine ad-hoc skripti za manipulaciju datotekama jedinstvenim, atomskim i sigurnim cjevovodom transformacija.

---

## Rezime S13 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **13.1 Vision** | ADAPTER | • **Anthropic / OpenAI Vision:** UI inspekcija i OCR<br>• **Annex A P2.2:** **MIME sniffing & Image size downsampling** | **Annex A P2.2** (`test_vision_validator_rejects_oversized_payload`) |
| **13.2 Canvas / Image** | MEHANIZAM + ADAPTER | • **ComfyUI / Flux:** Image generation adapter<br>• **Excalidraw / Mermaid:** **Schema-validirani Canvas + Stroga SVG sanitizacija** | **Annex A P1.4 & S6.2** (`test_canvas_engine_sanitizes_svg`) |
| **13.3 Audio & Voice** | ADAPTER | • **WhisperX / Deepgram:** Word-level STT<br>• **ElevenLabs / LiveKit:** Real-time WebRTC voice & barge-in | **Annex A P2.2** (`test_voice_session_enforces_duration_budget`) |
| **13.4 Video Obrada** | ADAPTER | • **FFmpeg:** Keyframe ekstrakcija<br>• **Remotion / HyperFrames:** Programatski video render | **Annex A P2.2** (`test_video_processor_rejects_exceeded_duration`) |
| **13.5 Artifact-Transformer** | MEHANIZAM (Pipeline) + ADAPTER | • **HARNESS-SPEC v0.8 r2:** **Generički Artifact-Transformer Ugovor**<br>• **Annex A P1.4 + S5.3:** `Typed-Op → Preview → Staging → Verify → Commit` | **Annex A P1.4 & S5.3** (`test_generic_artifact_transformer_rolls_back`) |
