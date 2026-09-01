PLAN S13

# S13 — Multimodal (`vision` / `image-out` / `voice` / `video` / `documents`)

## Zajednički vlasnički rez

- **Kod jezgre:** jedan `MediaGate` za byte-cap prije dekodiranja, detekciju stvarnog formata,
  dimenzije/trajanje/frame/page capove, provenance/trust/tenant oznake, consent i S6.3/S6.7
  egress odluku; privatni staging, S7 budget/cancel te verifikaciju outputa prije commita.
- **Vanjski adapteri:** vision modeli, ComfyUI/InvokeAI/diffusers, STT/TTS/realtime transport,
  FFmpeg/WhisperX/Remotion te pypdf/OCRmyPDF/pyHanko. Adapter nikad nije policy owner i ne
  dobiva nevalidirane putanje ni neograničen stream.
- **Podaci:** adapter manifesti, podržani MIME/operacije, model/workflow ID-i, voice ID-i,
  scene i dokument-op payloadi te deployment-specific capovi. Data smije samo suziti kernel max.
- **Paket-layout:** `internal/media/{artifact,gate,consent,staging}` +
  `internal/multimodal/{vision,image,canvas,voice,video,document}` + `adapters/*`. Svaki flag
  aktivira samo svoj adapter closure kroz S0.5; zajednički `MediaGate` nije capability flag.

```go
type MediaArtifact struct {
    Ref              contracts.ArtifactRef
    DeclaredMIME     string
    DetectedMIME     string
    SizeBytes        int64
    Width, Height    uint32
    Frames           uint64
    Duration         time.Duration
    SHA256           [32]byte
    TenantID         string
    Principal        contracts.PrincipalRef
    Trust            contracts.TrustClass
    Sensitivity      contracts.Sensitivity
    Lineage          []contracts.LineageEdge
}

type MediaLimits struct {
    MaxBytes, MaxDecodedBytes int64
    MaxWidth, MaxHeight       uint32
    MaxPixels, MaxFrames      uint64
    MaxDuration               time.Duration
}

type BackendDescriptor struct {
    ID, Version string
    Local       bool
    MIME        []string
    Operations  []string
    DataPolicy  provider.DataProcessingDescriptor
}

type MediaGate interface {
    Admit(context.Context, contracts.ArtifactRef, MediaLimits) (MediaArtifact, error)
    AuthorizeEgress(context.Context, MediaArtifact, BackendDescriptor,
        contracts.Purpose) (provider.DataPermit, error)
    VerifyOutput(context.Context, contracts.ArtifactRef, MediaLimits) (MediaArtifact, error)
}
```

**Globalne invarijante S13:**

1. Ekstenzija, korisnički `Content-Type` i adapterov odgovor su tvrdnje, ne dokaz. Jezgra čita
   `cap+1`, odbija prekoračenje, detektira magic/container i zatim parsira u izoliranom bounded
   procesu kada parser nije memorijski siguran. SVG/HTML/PDF ostaju aktivni, untrusted sadržaj.
2. Dimenzijski capovi primjenjuju se prije pune rasterizacije; encoded-byte cap nije obrana od
   decompression bomba. Audio/video imaju i duration/frame/sample cap, dokumenti page/object cap.
3. Remote backend poziv zahtijeva S6.3 allowlist, P2.5 data-processing/residency preflight i
   eksplicitni permit za osjetljive slike, glas, lice ili dokument. Consent je vezan uz digest,
   svrhu, backend i rok; nije globalni checkbox.
4. Adapter ne piše konačni target. Output ide u privatni same-filesystem staging, prolazi
   nezavisni `MediaGate.VerifyOutput`, pa S5.3 commit. Retry/deadline/cancel jedino posjeduje S7.
5. Modelov opis slike, transkript, OCR, labela, link ili metadata ulaze kao P0.1 untrusted
   `ContextBlock`; nikad se ne promoviraju u instrukciju niti dokazuju uspjeh operacije.
6. P2.2 budžet obuhvaća encoded i decoded byteove, piksele/frameove/stranice, CPU/RAM/disk,
   proces/socket broj i provider trošak. Odbijanje je prije side-effecta kad god je moguće.

## 13.1 Vision input

- **Tip:** ADAPTER ZA VISION BACKEND + MEHANIZAM VALIDACIJE/PROVENANCE

- **Aspekti:** browser-use daje koristan par screenshot + strukturirani DOM i eksplicitno računa
  CSS/device-pixel odnos; Open Interpreter daje vision u computer-use toku; OpenHands veže vision
  observation uz browsing petlju. Najbolji graft nije novi browser runtime: S4.5 posjeduje capture,
  S13 validira artefakt, a S2 provider capability daje vision inference. Naš dodatak je čuvanje
  screenshota i DOM-a kao dva odvojena lineage bloka s istim capture ID-em, bez lažne tvrdnje da
  se međusobno potvrđuju.

- **Sinteza:** `VisionBackend` prima samo admitted `ImageInput`; lokalni ili remote adapter oglašava
  stvarno izmjerene formate i maxove kroz S0.3. `VisionRequest` može povezati screenshot s DOM
  snapshotom, viewportom i DPR-om, ali rezultat navodi izvor za svaku grounding referencu. S4.5
  dostavlja capture; provider-native multimodal API je primarni backend kroz S2.1/S2.6, a lokalni
  VLM je isti interface. SVG se prije vision poziva renderira u sandboxu u raster; originalni SVG
  ostaje zaseban untrusted artefakt.

```go
type ImageInput struct {
    Media MediaArtifact
    CaptureID string
    Viewport struct{ CSSWidth, CSSHeight uint32; DevicePixelRatio float64 }
    DOM *contracts.ContextBlock
}

type VisionRequest struct {
    Images []ImageInput
    Task contracts.ContextBlock
    OutputSchema contracts.SchemaRef
    MaxOutputTokens uint32
}

type VisionResult struct {
    Content contracts.ContextBlock
    Grounding []struct{ CaptureID, RegionRef, DOMRef string; Confidence float32 }
    Usage provider.Usage
}

type VisionBackend interface {
    Descriptor() BackendDescriptor
    Analyze(context.Context, VisionRequest, provider.AttemptGrant) (VisionResult, error)
}
```

  Invarijante: (1) JPEG/PNG/WebP magic i dekoder moraju se slagati s dopuštenim MIME-om;
  (2) `width*height*frames` provjerava overflow i cap prije allocationa; (3) DOM i pixels zadržavaju
  vlastiti trust/lineage; (4) vision odgovor ne daje tool authority; (5) remote poziv bez digest-bound
  data permite faila prije upload-a.

- **Salvage:** `core/vision.py:17-24` portirati response `cap+1`; HTTP pozive iz
  `core/vision.py:37-68` preseliti iza S6.3 egress/S2 providera. **Ne portirati**
  `core/vision.py:31-34`: MIME se izvodi iz ekstenzije, nepoznato postaje JPEG, a čitanje točno
  `cap` byteova može tiho skratiti veći input. Browser capture/DOM dolazi iz S4.5, ne iz ove jezgre.

- **Ugovor/RED:** P0.1/P0.3, P2.2 i P2.5. RED:
  `test_vision_rejects_extension_spoof_and_truncated_cap_read` stvara `.jpg` s PNG/garbage magicom
  i datoteku `cap+1`; oba moraju biti odbijena bez provider poziva. Dodatno:
  `test_pixel_bomb_is_rejected_before_decode_allocation`,
  `test_remote_vision_requires_digest_bound_data_permit`,
  `test_dom_instruction_cannot_gain_trust_through_screenshot_pairing` i
  `test_vision_text_cannot_authorize_tool_call`.

- **Verifikacija:** browser-use kod stvarno ima screenshot API, DOM analizu i DPR/viewport
  korekciju ([official source](https://github.com/browser-use/browser-use/blob/main/browser_use/dom/service.py));
  to potvrđuje capture-aspekt, ne njegovu sigurnost ni točnost semantičkog groundinga. Lokalni
  `vision.py` dokazuje bounded response, ali i gore navedeni input gap. Open Interpreter/OpenHands
  integraciju treba prije matrice potvrditi executable fixtureom; spec-lista sama nije dokaz.

- **Pareto floor:** **PASS na dizajnu; backend quality je matrix-gate.** Zadržava screenshot+DOM i
  computer-use primjene, ali dodaje typed provenance, decoder cap i consent. Najslabija tvrdnja je
  grounding kvaliteta pojedinog modela; aktivacija zahtijeva fixture s koordinatama, OCR-om,
  dijagramima i adversarialnim tekstom, ne README.

## 13.2 Generacija/edit slika + strukturirani canvas

- **Tip:** ADAPTER ZA GENERACIJU/EDIT + MEHANIZAM CANVASA I OUTPUT COMMITA

- **Aspekti:** ComfyUI daje graph/workflow backend i durable job/output identitete; InvokeAI daje
  produkcijski API/UI tok; diffusers daje najnižu programatsku komponentu. NEXUS `imagegen.py`
  dodaje bounded polling/download, validation-before-replace i SSRF gate. Za canvas NEXUS daje
  kompaktni scene JSON, Excalidraw editabilni model, Mermaid text-first ergonomiju. Primarni image
  backend je ComfyUI adapter; primarni canvas je naš mali `SceneV1` renderer, ne ugrađeni ComfyUI.

- **Sinteza:** odvojiti `ImageGenerator`, `ImageEditor` i `DiagramRenderer`; tako adapter koji samo
  generira ne laže da može mask/inpaint. ComfyUI workflow je pinned, schema-validiran adapter data;
  proizvoljni custom-node graph nije trusted input. Submit/poll/cancel koristi S7 attempt/job state,
  idempotency i bounded budget. Remote result URL prolazi S6.3 egress kao novi untrusted target.

```go
type ImageSpec struct { Width, Height uint32; Count uint16; Format string }
type ImageEdit struct { Source, Mask MediaArtifact; Prompt contracts.ContextBlock; Spec ImageSpec }
type GeneratedImage struct { Artifact MediaArtifact; Seed string; BackendJobID string; RecipeDigest string }

type ImageGenerator interface {
    Descriptor() BackendDescriptor
    Generate(context.Context, contracts.ContextBlock, ImageSpec,
        provider.AttemptGrant) ([]GeneratedImage, error)
}
type ImageEditor interface {
    Descriptor() BackendDescriptor
    Edit(context.Context, ImageEdit, provider.AttemptGrant) ([]GeneratedImage, error)
}

type SceneV1 struct { Version string; Width, Height uint32; Nodes []SceneNode; Edges []SceneEdge }
type DiagramRenderer interface {
    Validate(SceneV1, MediaLimits) error
    Render(context.Context, SceneV1, OutputKind) (staging.StagedArtifact, error)
}
```

  `SceneV1` dopušta zatvoren skup primitiva, brojeva, boja, plain-text labela i internog node-ID
  povezivanja; nema raw SVG/HTML/CSS/filter/path-command/URL polja. Renderer generira SVG, ne
  interpolira markup, zatim ga sanitizira i sandboxirano rasterizira; PNG je preview, SceneV1 je
  editabilni source. **Ovo smanjuje napadnu površinu, ali nije tvrdnja da je canvas injection-proof:**
  labela i link mogu ostati semantički zlonamjerni, a SVG je aktivni format te ostaje untrusted.

- **Salvage:** `core/imagegen.py:24-32,73-82,107-165,176-222` Python→Go portirati hard capove,
  `cap+1`, egress-gated download, privatni workdir, dekoder-verifikaciju i same-dir atomic replace.
  Iz `core/canvas.py:54-56,70-268,386-453` portirati element/point/data capove, numeričku normalizaciju,
  XML escaping i JSON→SVG/PNG tok. Ne portirati implicitno prihvaćanje proizvoljnog ComfyUI node
  grafa; workflow mora biti pinned/allowlisted i supply-chain skeniran kroz S6.8.

- **Ugovor/RED:** P0.1, P1.4 za billable/remote job i P2.2/P2.5; S5.3 za output. RED:
  `test_image_output_is_not_committed_before_independent_decode` vraća HTML/oversize/bomb pod
  `.png`; target ostaje byte-identičan. Dodatno:
  `test_remote_result_url_reenters_egress_gate`,
  `test_unpinned_comfy_workflow_is_rejected`,
  `test_canvas_raw_markup_and_external_href_are_rejected`,
  `test_canvas_label_is_escaped_but_remains_untrusted` i
  `test_cancelled_generation_cannot_commit_late_result`.

- **Verifikacija:** ComfyUI službeni API stvarno prima workflow graph, vraća job/prompt identitet i
  dohvatljive outpute ([OpenAPI v2](https://github.com/Comfy-Org/docs/blob/main/openapi-v2.yaml));
  to opravdava adapter, ne dopuštanje arbitrary custom nodeova. Lokalni imagegen/canvas imaju
  stvarne cap/atomic/render mehanizme. Hipoteza da SceneV1 troši manje tokena od SVG-a ostaje
  **neprovjerena** do mjerenja istih 30 dijagrama: median input+repair tokena i semantic-equality
  checker; ne ulazi u Pareto tvrdnju prije tog testa.

- **Pareto floor:** **PASS za granice i zamjenjivost; generation quality nije dokazana.** Sinteza
  zadržava ComfyUI graph moć, InvokeAI/diffusers adapterability i editabilni canvas, uz jači
  staged-verify-commit. Ako pinned workflow ne može izraziti potreban edit, dodaje se novi potpisani
  recipe manifest, ne raw graph escape hatch.

## 13.3 Audio/voice (STT + TTS + realtime)

- **Tip:** ADAPTERI ZA STT/TTS/TRANSPORT + MEHANIZAM SESSION/BUDGET/CONSENT GRANICE

- **Aspekti:** Pipecat daje frame/pipeline model za realtime voice; LiveKit Agents razlikuje
  sekvencijalni STT→LLM→TTS, realtime i hibridni pipeline; speaches daje lokalni kompatibilni
  STT/TTS server. WhisperX je batch STT/alignment/diarization adapter, koristan i 13.4. NEXUS voice
  daje strukturirani argv, timeout i kill-tree. Ne uvodimo jedan lažni `VoiceBackend`: STT, TTS i
  transport imaju zasebne capability interfacee, a `VoiceSession` ih komponira.

- **Sinteza:** file input prolazi `MediaGate`; live input dobiva eksplicitni capture grant,
  vidljiv recording-state i bounded frame queue. Barge-in samo emitira S7 cancel trenutnom TTS/turnu.
  Partial transcript je untrusted observation; final transcript dobiva segment/word timestamps i
  confidence, nikad status “user approved”. TTS output ide u staging, dekodira se i mjeri prije play/
  commit-a. Remote STT/TTS koristi P2.5 permit; voice cloning/enrollment je zaseban high-risk effect i
  nije implicitno dio `voice` flaga.

```go
type AudioFrame struct { Seq uint64; PCM []byte; SampleRate uint32; Channels uint8; CapturedAt time.Time }
type Transcript struct { Text contracts.ContextBlock; Segments []SpeechSegment; Final bool }
type SpeechToText interface {
    Descriptor() BackendDescriptor
    Transcribe(context.Context, MediaArtifact, STTOptions, provider.AttemptGrant) (Transcript, error)
}
type TextToSpeech interface {
    Descriptor() BackendDescriptor
    Synthesize(context.Context, contracts.ContextBlock, TTSOptions,
        provider.AttemptGrant) (staging.StagedArtifact, error)
}
type VoiceTransport interface {
    Open(context.Context, CaptureGrant) (frames <-chan AudioFrame, close func() error, err error)
}
type VoiceSession interface { Run(context.Context, VoiceTransport, SpeechToText, TextToSpeech) error }
```

  Queue je byte+duration bounded, koristi backpressure/drop policy koja nikad ne skriva gap
  (emitira `audio.frames_dropped`). Cancel zatvara capture, mrežu i child procese te drain-a kanale.
  Max decoded samples računa se prije allocationa; container duration potvrđuje decoder/probe.

- **Salvage:** `core/voice.py:20-56` portirati structured argv, process-group timeout/kill i bounded
  transcript file read. **Ne portirati** API path koji cijeli audio učita prije cap-a niti TTS path
  koji čita fiksni limit bez `cap+1`, piše ravno na target i provjerava samo non-empty
  (`core/voice.py:61-75,102-133`). Zamijeniti zajedničkim MediaGate+staging. WhisperX ostaje
  vanjski Python adapter; nema potrebe portati ML stack u Go.

- **Ugovor/RED:** P0.1/P0.2, P1.4 za voice enrollment/cloning ako se ikad uključi, P2.2/P2.5 i
  P2.1 za purge snimki. RED: `test_microphone_cannot_open_without_capture_grant` mora dokazati nula
  frameova i nula socketa. Dodatno:
  `test_audio_duration_bomb_rejected_before_stt`,
  `test_tts_garbage_never_replaces_target`,
  `test_barge_in_cancels_once_through_s7_and_drains`,
  `test_remote_stt_without_residency_permit_sends_zero_bytes` i
  `test_data_purge_removes_recording_while_memory_forget_does_not_claim_deletion`.

- **Verifikacija:** LiveKit službene upute potvrđuju STT/LLM/TTS, realtime i half-cascade pipeline
  varijante ([pipeline docs](https://docs.livekit.io/agents/models/pipelines/)). WhisperX kod stvarno
  izlaže forced alignment i diarization opcije
  ([official source](https://github.com/m-bain/whisperX/blob/main/whisperx/__main__.py)), ali otvoren
  timestamp problem znači da word timing nije ground truth; mora nositi confidence i fixture WER/
  alignment mjerenje. Pipecat/speaches licence i trenutni API pin provjeravaju se u matrici.

- **Pareto floor:** **PASS na kompoziciji i safetyju; latency/accuracy uvjetni.** Nadmašuje
  monolitni voice adapter po zamjenjivosti i cancel/consent dokazivosti. Realtime floor se priznaje
  tek kad p95 mouth-to-ear, interruption latency, packet loss i cleanup prođu na 3 OS-a.

## 13.4 Video obrada i razumijevanje

- **Tip:** ADAPTERI ZA PROCESS/UNDERSTANDING/GENERATION + MEHANIZAM TYPED-OP GRANICE

- **Aspekti:** FFmpeg je primarni deterministički processor; WhisperX daje transcript, word
  alignment i diarization; ComfyUI daje video generation workflowe. Remotion/HyperFrames su
  runner-up za data/HTML/React→video, ali ostaju vanjski Node/browser adapter, ne dio Go jezgre.
  `yt-dlp` ostaje ingestion utility i ne ulazi u processor ranking.

- **Sinteza:** razdvojiti `VideoProcessor`, `VideoUnderstanding` i `VideoGenerator`. Processor prima
  zatvoreni `VideoOp` enum i typed parametre; nema raw ffmpeg args/filtergraph. Input se lstat-a,
  magic/container probira, zatim child sandbox dopušta samo potrebne lokalne file descriptore i
  `file` protokol. Frame extraction ima count/dimension/total-byte cap. Generation job prati isti
  P1.4/S7 submit→poll→reconcile tok kao 13.2; output se ffprobe/decode verificira prije commita.

```go
type VideoOpKind uint8 // Transcode, Trim, Scale, Crop, Rotate, ExtractFrames, AddAudio, Subtitle
type VideoOp struct { Kind VideoOpKind; Params json.RawMessage; SchemaVersion uint16 }
type VideoProcessor interface {
    Descriptor() BackendDescriptor
    Plan(MediaArtifact, VideoOp) (ArtifactPlan, error)
    Stage(context.Context, ArtifactPlan, budget.GrantRef) (staging.StagedArtifact, error)
}
type VideoUnderstanding interface {
    Analyze(context.Context, MediaArtifact, UnderstandingOptions,
        provider.AttemptGrant) (Transcript, []FrameObservation, error)
}
type VideoGenerator interface {
    Generate(context.Context, VideoRecipe, provider.AttemptGrant) (staging.StagedArtifact, error)
}
```

  FFmpeg adapter pinna binary digest/version, izgradi argv iz enum-a, postavlja `-nostdin` i
  `-protocol_whitelist file`, ne nasljeđuje env/secrets, te radi pod S6.2/S7. Remotion HTML/JS je
  aktivni program: samo signed recipe u browser sandboxu bez mreže po defaultu; user HTML nije
  “video data”.

- **Salvage:** **portirati gotovo 1:1 uz parity test** `core/video.py:25-31,74-176,247-338`:
  input/output capove, regular-file/no-symlink guard, protocol whitelist, demuxer probing,
  isolated ffmpeg, typed filter builder, frame cap i staged verification/replace. Zadržati poštenu
  napomenu iz `core/video.py:15-18`: jedan whitelisted demuxer i native decoder i dalje su parser
  rizik. `core/imagegen.py:234-267` daje bounded async video polling. Ne portirati raw filter escape.

- **Ugovor/RED:** P0.1/P0.2, P1.4 za remote generation, P2.2/P2.5 i S5.3. RED:
  `test_video_nested_network_protocol_is_blocked` daje HLS/concat/URL-smuggling input i očekuje
  nula mrežnih konekcija. Dodatno:
  `test_raw_filtergraph_is_unrepresentable`,
  `test_timeout_kills_ffmpeg_tree_and_preserves_target`,
  `test_frame_extraction_respects_count_pixel_and_total_byte_caps`,
  `test_invalid_generated_video_never_commits` i
  `test_late_remote_job_result_after_cancel_is_fenced`.

- **Verifikacija:** FFmpeg službena dokumentacija potvrđuje da `protocol_whitelist` postavlja listu
  dopuštenih protokola i da su inače protokoli široko omogućeni
  ([official docs](https://ffmpeg.org/ffmpeg-protocols.html)); zato je allowlist stvaran graft, ali
  nije potpuni sandbox. WhisperX source potvrđuje VAD/alignment/diarization, a lokalni `video.py`
  potvrđuje typed filters i capove. Remotion/HyperFrames i ComfyUI video backend prolaze pinned
  executable fixture/licence provjeru prije promicanja.

- **Pareto floor:** **PASS za determinističku obradu; conditional za understanding/generation.**
  FFmpeg floor dobiva jači typed/sandbox/atomic omotač bez gubitka operacija. Najjači
  counterexample je 0-day u native demuxeru: per-process sandbox, minimalni codec/demuxer build i
  resource cap smanjuju blast radius, ali plan ne tvrdi parser safety.

## 13.5 Dokument-mutacija — instanca generičkog Artifact-transformera

- **Tip:** MEHANIZAM GENERIČKOG TRANSAKCIJSKOG UGOVORA + FORMATSKI ADAPTERI

- **Aspekti:** pypdf je primarni PDF structure adapter, OCRmyPDF OCR adapter, pyHanko signature
  sign/verify adapter. Vrijedni aspekt nije popis PDF glagola nego jedan format-neutralni tok:
  typed op → deterministic preview/dry-run → privatni staging → independent verify → commit ili
  rollback/compensate. PDF/DOCX/spreadsheet/image samo implementiraju adapter ABI; read ingestion
  ostaje S10.1.

- **Sinteza:** kernel posjeduje `ArtifactTransformer`, state-machine i receipts; adapter posjeduje
  parser/serializer i operation schema. Preview veže source/target expected hash, op digest,
  adapter digest i očekivane posljedice (uključujući gubitak potpisa/makroa/metadata). ApprovalGrant
  je P1.4 exact-binding nad tim digestom. `DryRun` nema write capability za target. `Stage` piše u
  privatni same-filesystem staging, `Verify` koristi nezavisno ponovno otvaranje/parser i typed
  postconditions, pa kernel radi S5.3 first-writer-wins commit.

```go
type ArtifactDescriptor struct {
    Ref contracts.ArtifactRef
    DetectedMIME, FormatVersion string
    SHA256 [32]byte
    Signed, Encrypted, HasMacros bool
    TenantID string
}
type ArtifactOperation struct { Kind string; SchemaVersion uint16; Args json.RawMessage }
type ArtifactPlan struct {
    Intent effects.EffectIntent
    Source ArtifactDescriptor
    TargetPath, ExpectedTargetHash string
    OperationDigest, AdapterDigest string
    Predicted []ArtifactDelta
}
type ArtifactAdapter interface {
    Descriptor() BackendDescriptor
    ValidateOp(ArtifactDescriptor, ArtifactOperation) error
    Preview(context.Context, ArtifactDescriptor, ArtifactOperation) ([]ArtifactDelta, error)
    Stage(context.Context, ArtifactPlan, staging.Dir, budget.GrantRef) (StagedArtifactSet, error)
    Verify(context.Context, ArtifactPlan, StagedArtifactSet) (VerificationReceipt, error)
}
type ArtifactTransformer interface {
    Plan(context.Context, contracts.ArtifactRef, string, ArtifactOperation) (ArtifactPlan, error)
    DryRun(context.Context, ArtifactPlan) (PreviewReceipt, error)
    Commit(context.Context, ArtifactPlan, StagedArtifactSet,
        effects.ApprovalGrant) (CommitReceipt, error)
    Reconcile(context.Context, string) (effects.EffectState, error)
}
```

  Stanja su P1.4 `DRAFT→VALIDATED→APPROVAL_PENDING→APPROVED→COMMITTING→COMMITTED|UNKNOWN`,
  zatim `VERIFIED` ili compensation grana. Intent je durable prije učinka; timeout nakon mogućeg
  replacea postaje `UNKNOWN`, ne retry. Jedan output koristi same-directory temp+atomic replace.
  **Filesystem nema opći atomski multi-file replace:** v1 zato ili (a) vraća jedan container,
  (b) commita novu immutable directory-generation pa atomski mijenja manifest pointer, ili
  (c) odbija `ATOMIC_MULTI_OUTPUT_UNSUPPORTED`; nikad ne obećava transakciju per-file petljom.
  Mutacija potpisanog dokumenta mora u previewu eksplicitno pokazati signature invalidation;
  potpisivanje/validacija ide kroz pyHanko adapter i zaseban approval.

- **Salvage:** `core/pdf.py:434-504` portirati bounded read, page cap, same-dir staging i atomic
  replace kao PDF adapter helper; postojeće merge/rotate/watermark/form/OCR operacije mapirati na
  typed opove tek nakon parity fixturea. `core/pdf.py:546-578` izričito priznaje da split commitira
  per-file i može ostaviti parcijalan rezultat nakon pada: **to ne portirati kao zadovoljavanje
  ugovora**, nego vratiti ZIP/directory-generation ili odbiti. `core/pdf.py` i Python dependencyji
  ostaju optional subprocess/service adapteri; `documents` closure nije ACTIVE ako runtime nije
  prisutan i verificiran—single-binary jezgra ne glumi da sadrži pypdf.

- **Ugovor/RED:** P1.4 je glavni gate; P0.1/P0.3, P2.2, S5.3 i P2.5 kad dokument ide remote.
  Kanonski RED `test_approval_cannot_authorize_modified_effect`: nakon approvala promijeni op args,
  source hash, target ili adapter digest; commit mora odbiti prije writea. Dodatno:
  `test_dry_run_has_zero_target_writes`,
  `test_source_or_target_change_after_preview_blocks_commit`,
  `test_verify_failure_preserves_old_target_and_staging_evidence`,
  `test_signed_document_requires_disclosed_signature_delta`,
  `test_multi_output_cannot_partially_commit` i
  `test_timeout_after_replace_enters_unknown_and_reconciles_without_retry`.

- **Verifikacija:** pypdf dokumentacija potvrđuje merge/rotation/forms/encryption površine
  ([official docs](https://pypdf.readthedocs.io/)); to dokazuje formatski adapter, ne atomicnost ni
  sandbox. Lokalni `pdf.py` stvarno ima caps/atomic single-file write i pošteno dokumentiran split
  gap. pyHanko/OCRmyPDF moraju proći pinned licence/API i malicious-fixture sandbox test prije
  activationa; uspješan parse nije dokaz očuvanja semantike ili potpisa.

- **Pareto floor:** **PASS samo uz generički contract kao owner.** Dobiva pypdf/OCR/signature
  sposobnosti bez anatomije po formatu i jaču preview/approval/atomic granicu. Najjači
  counterexample je multi-file DOCX/PDF split na postojećem targetu; eksplicitno ograničenje na
  container/generation ili fail-closed čuva istinitost ugovora umjesto lažne “atomic” oznake.

## S13 verifikacijski i aktivacijski gate

| Flag | Obvezni fixture prije `ACTIVE` | Vlasnički gate |
|---|---|---|
| `vision` | spoofed MIME, pixel bomb, screenshot+DOM mismatch, no-consent remote | MediaGate + P2.5 |
| `image-out` | invalid/oversize output, SSRF result URL, malicious SceneV1 labels, late result | S6.3 + S5.3 + S7 |
| `voice` | duration bomb, mic-consent, backpressure, barge-in cleanup, invalid TTS | P2.2 + P2.5 + S7 |
| `video` | nested protocol, decoder crash/hang, frame cap, invalid output, stale remote job | S6.2 + P2.2 + S7 |
| `documents` | modified approval, dry-run write, signed-input delta, crash/partial multi-output | P1.4 + S5.3 |

**Pareto-floor sud:** backend capability se priznaje tek kad executable adapter fixture potvrdi
deklarirani op, cancel, cap i output verifier. README/API prisutnost potvrđuje da je aspekt stvaran,
ali ne da je dovoljno siguran ili kvalitetan. S13 kao cjelina je **PASS na buildable dizajnu** uz
tri eksplicitna proof-ceilinga: semantička canvas injekcija nije riješena samom shemom; native
media parseri nisu dokazano sigurni samo zbog allowlista; kvaliteta vision/STT/generation backendova
ostaje matrica, ne arhitektonska tvrdnja.
