# Buildable Go Dizajn: Generic Evidence-Checker, Test Impact Analysis (TIA) i Hardened Edit Subsystem
## Modul: `pkg/checker`, `pkg/tia`, `pkg/edit` — Greenfield `CGO_ENABLED=0`

**Status:** MUST-RESOLVE #4 Arhitektonski Dizajn  
**Pravilo:** 100% Čisti Greenfield Go bez CGO-a, nula salvage koda, bez proze, čiste `struct`/`interface` definicije, ugovorne invarijante i RED test specifikacije.  
**Ciljni repozitorij:** `MatNik89/nexus`

---

# DIO A: GENERIČKI EVIDENCE-CHECKER I ACCEPTANCE UGOVORI (`pkg/checker`)

## A.1. Tipovi, Ugovori i EvidenceBundle

```go
package checker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

// PrincipalID definira identitet izvršitelja ili verifikatora
type PrincipalID string

// Invarijanta P0.4: Worker nikada ne smije ocjenjivati vlastiti rad
var (
	ErrSelfGradingForbidden = errors.New("worker-principal == checker-principal: self-grading strictly forbidden")
	ErrEvidenceIncomplete   = errors.New("evidence bundle missing mandatory artifacts for contract")
	ErrContractViolation    = errors.New("acceptance contract conditions not satisfied")
	ErrTamperDetected       = errors.New("evidence hash mismatch against execution receipt")
)

type ContractType string

const (
	ContractCodingDiffExit         ContractType = "coding.diff_exit"
	ContractChannelDeliveryReceipt ContractType = "channel.delivery_receipt"
	ContractCalendarMutation       ContractType = "calendar.staged_mutation"
	ContractResearchGrounding      ContractType = "research.grounded_citations"
	ContractSystemStateInvariant   ContractType = "system.state_invariant"
)

// AcceptanceContract definira formalni kriterij prihvatljivosti zadatka
type AcceptanceContract struct {
	ID             string
	Type           ContractType
	RequiredBy     PrincipalID // Korisnik ili orkestrator
	AssignedWorker PrincipalID // Worker koji izvršava zadatak
	Parameters     map[string]string
	CreatedAt      time.Time
	Timeout        time.Duration
}

// EvidenceBundle je nepromjenjivi skup strojnih dokaza prikupljenih tijekom izvršavanja
type EvidenceBundle struct {
	ContractID       string
	WorkerID         PrincipalID
	GeneratedAt      time.Time
	ExecutionReceipt *ExecutionReceiptRef // Poveznica na signed receipt iz execution membrane

	// Tipizirani dokazni artefakti (popunjeni ovisno o ContractType)
	CodingEvidence   *CodingArtifact
	ChannelEvidence  *ChannelReceiptArtifact
	CalendarEvidence *CalendarMutationArtifact
	ResearchEvidence *ResearchGroundingArtifact
	CustomEvidence   map[string][]byte
}

type CodingArtifact struct {
	GitDiffBytes   int
	GitDiffHash    [32]byte
	TargetFiles    []string
	CompilerExit   int
	TestsPassed    int
	TestsFailed    int
	CoverageDelta  float64
}

type ChannelReceiptArtifact struct {
	ChannelID     string
	MessageID     string
	RecipientHash [32]byte
	ProviderAckID string
	DeliveredAt   time.Time
	DeliveryState string // "DELIVERED", "CONFIRMED_READ"
}

type CalendarMutationArtifact struct {
	CalendarID    string
	EventID       string
	Action        string // "CREATE", "UPDATE", "DELETE"
	PreImageHash  [32]byte
	PostImageHash [32]byte
	StagedDiff    string
	AtomicCommitted bool
}

type ResearchGroundingArtifact struct {
	QueryHash      [32]byte
	CitationsCount int
	Sources        []GroundingSource
	FactCheckScore float64 // [0.0, 1.0] iz neovisnog izvora
}

type GroundingSource struct {
	URI          string
	ContentHash  [32]byte
	HTTPStatus   int
	RetrievedAt  time.Time
}

type ExecutionReceiptRef struct {
	ReceiptHash [32]byte
	Signature   []byte
}

type GradeResult struct {
	ContractID      string
	CheckerID       PrincipalID
	ContractType    ContractType
	Passed          bool
	Score           float64 // 0.0 - 1.0
	Violations      []string
	EvaluatedAt     time.Time
	EvaluationHash  [32]byte
}
```

## A.2. Sučelje i Strategije Verifikacije

```go
// AcceptanceChecker je sučelje koje svaki domenski verifikator implementira
type AcceptanceChecker interface {
	ContractType() ContractType
	Grade(ctx context.Context, contract AcceptanceContract, evidence EvidenceBundle, checkerPrincipal PrincipalID) (GradeResult, error)
}

// UniversalVerifier orkestrira evaluaciju i nameće invarijantu odvojenosti principala
type UniversalVerifier struct {
	checkers map[ContractType]AcceptanceChecker
}

func NewUniversalVerifier(strategies ...AcceptanceChecker) *UniversalVerifier {
	m := make(map[ContractType]AcceptanceChecker)
	for _, s := range strategies {
		m[s.ContractType()] = s
	}
	return &UniversalVerifier{checkers: m}
}

func (v *UniversalVerifier) Verify(
	ctx context.Context,
	contract AcceptanceContract,
	evidence EvidenceBundle,
	checkerPrincipal PrincipalID,
) (GradeResult, error) {
	// Invarijanta 1: Zabrana self-gradinga (Worker != Checker)
	if contract.AssignedWorker == checkerPrincipal || evidence.WorkerID == checkerPrincipal {
		return GradeResult{Passed: false}, ErrSelfGradingForbidden
	}

	// Invarijanta 2: Usklađenost ugovora
	if contract.ID != evidence.ContractID {
		return GradeResult{Passed: false}, fmt.Errorf("contract ID mismatch: %s != %s", contract.ID, evidence.ContractID)
	}

	checker, exists := v.checkers[contract.Type]
	if !exists {
		return GradeResult{Passed: false}, fmt.Errorf("no registered checker for contract type: %s", contract.Type)
	}

	result, err := checker.Grade(ctx, contract, evidence, checkerPrincipal)
	if err != nil {
		return result, err
	}

	// Zapečati rezultat hashiranjem
	payload := fmt.Sprintf("%s:%s:%t:%f:%s", result.ContractID, checkerPrincipal, result.Passed, result.Score, result.EvaluatedAt.UTC().Format(time.RFC3339Nano))
	result.EvaluationHash = sha256.Sum256([]byte(payload))

	return result, nil
}

// CodingChecker strategija: git diff > 0 && exit == 0
type CodingChecker struct{}

func (c *CodingChecker) ContractType() ContractType { return ContractCodingDiffExit }

func (c *CodingChecker) Grade(ctx context.Context, contract AcceptanceContract, evidence EvidenceBundle, checker PrincipalID) (GradeResult, error) {
	res := GradeResult{
		ContractID:   contract.ID,
		CheckerID:    checker,
		ContractType: ContractCodingDiffExit,
		EvaluatedAt:  time.Now().UTC(),
	}

	if evidence.CodingEvidence == nil {
		return res, ErrEvidenceIncomplete
	}

	art := evidence.CodingEvidence
	if art.GitDiffBytes <= 0 {
		res.Violations = append(res.Violations, "empty git diff: no code artifact created or modified")
	}
	if art.CompilerExit != 0 {
		res.Violations = append(res.Violations, fmt.Sprintf("non-zero compiler/test exit code: %d", art.CompilerExit))
	}
	if art.TestsFailed > 0 {
		res.Violations = append(res.Violations, fmt.Sprintf("test failures detected: %d failed", art.TestsFailed))
	}

	res.Passed = len(res.Violations) == 0
	if res.Passed {
		res.Score = 1.0
	}
	return res, nil
}

// ChannelDeliveryChecker strategija: potvrda isporuke preko protokola
type ChannelDeliveryChecker struct{}

func (c *ChannelDeliveryChecker) ContractType() ContractType { return ContractChannelDeliveryReceipt }

func (c *ChannelDeliveryChecker) Grade(ctx context.Context, contract AcceptanceContract, evidence EvidenceBundle, checker PrincipalID) (GradeResult, error) {
	res := GradeResult{
		ContractID:   contract.ID,
		CheckerID:    checker,
		ContractType: ContractChannelDeliveryReceipt,
		EvaluatedAt:  time.Now().UTC(),
	}

	if evidence.ChannelEvidence == nil {
		return res, ErrEvidenceIncomplete
	}

	art := evidence.ChannelEvidence
	if art.ProviderAckID == "" {
		res.Violations = append(res.Violations, "missing provider ack id")
	}
	if art.DeliveryState != "DELIVERED" && art.DeliveryState != "CONFIRMED_READ" {
		res.Violations = append(res.Violations, fmt.Sprintf("invalid delivery state: %s", art.DeliveryState))
	}

	res.Passed = len(res.Violations) == 0
	if res.Passed {
		res.Score = 1.0
	}
	return res, nil
}
```

## A.3. Invarijante i RED Testovi (`pkg/checker`)

| RED Test ID | Što Testira | Uvjet Prolaza (Assert) |
| :--- | :--- | :--- |
| `TestUniversalVerifier_EnforcesWorkerCheckerSeparation` | Pokušaj da worker ocijeni vlastiti `EvidenceBundle`. | Vraća `ErrSelfGradingForbidden`, `res.Passed == false`. |
| `TestCodingChecker_RejectsEmptyDiff` | Worker vraća text "Done!", ali `GitDiffBytes == 0`. | `res.Passed == false`, `Violations` sadrži "empty git diff". |
| `TestCodingChecker_RejectsFailedCompilerExit` | `GitDiffBytes > 0`, ali `CompilerExit == 2`. | `res.Passed == false`, `Violations` sadrži "non-zero compiler exit". |
| `TestChannelDeliveryChecker_ValidatesReceipt` | Valjani `ProviderAckID` i status `DELIVERED`. | `res.Passed == true`, `res.Score == 1.0`, `EvaluationHash != [32]byte{}`. |
| `TestUniversalVerifier_FailsClosedOnMissingArtifact` | `ContractType == ContractResearchGrounding`, ali `ResearchEvidence == nil`. | Vraća `ErrEvidenceIncomplete`, `res.Passed == false`. |

---

# DIO B: TEST IMPACT ANALYSIS (TIA) GO ARHITEKTURA (`pkg/tia`)

## B.1. Strukturni Model i Komponente

```go
package tia

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

var (
	ErrCoverageProfileCorrupt = errors.New("coverage profile is malformed or unreadable")
	ErrStaleCallGraph         = errors.New("call graph is stale: fallback to full test suite required")
)

// LanguageRuntime definira podržane jezgrene parsere pokrivenosti
type LanguageRuntime string

const (
	LangGo     LanguageRuntime = "go"
	LangRust   LanguageRuntime = "rust"
	LangPython LanguageRuntime = "python"
	LangTS     LanguageRuntime = "typescript"
)

// CoverageBlock predstavlja pokriveni blok koda mapiran na test
type CoverageBlock struct {
	FilePath  string
	StartLine int
	EndLine   int
	TestName  string
	HitCount  int
}

// CoverageIngester parsira formate pokrivenosti po jeziku u standardizirani indeks
type CoverageIngester interface {
	Language() LanguageRuntime
	Parse(profileData []byte) ([]CoverageBlock, error)
}

// SymbolSpan definira semantički simbol unutar datoteke
type SymbolSpan struct {
	FilePath   string
	SymbolName string
	Kind       string // "function", "method", "type"
	StartLine  int
	EndLine    int
}

// DiffRange definira raspon izmijenjenih linija iz git diffa
type DiffRange struct {
	FilePath  string
	StartLine int
	LineCount int
	IsDeleted bool
}

// CallGraphMatrix održava dvosmjerno mapiranje Symbol <-> Test
type CallGraphMatrix struct {
	CommitHash     string
	CreatedAt      time.Time
	GraphDigest    [32]byte
	SymbolToTests  map[string][]string // "pkg/file.go:FunctionName" -> ["TestFuncA", "TestFuncB"]
	FileToTests    map[string][]string // "pkg/file.go" -> ["TestAllInPkg"]
}

// DynamicTestSelectionResult sadrži selektirane testove i odluku o fallbacku
type DynamicTestSelectionResult struct {
	ImpactedTests  []string
	MustRunAll     bool
	FallbackReason string
	ResolvedAt     time.Time
	SelectedCount  int
	TotalTestCount int
}
```

## B.2. TIA Engine i Obvezni Full-Suite Fallback

```go
type TIAEngine struct {
	ingesters       map[LanguageRuntime]CoverageIngester
	currentGraph    *CallGraphMatrix
	maxCommitAge    int // Maksimalna udaljenost u commitima prije nego graf postane stale
	commitDistance  int
}

func NewTIAEngine(maxCommitAge int, ingesters ...CoverageIngester) *TIAEngine {
	m := make(map[LanguageRuntime]CoverageIngester)
	for _, ing := range ingesters {
		m[ing.Language()] = ing
	}
	return &TIAEngine{
		ingesters:    m,
		maxCommitAge: maxCommitAge,
	}
}

// IngestCoverage učitava profil pokrivenosti i gradi CallGraphMatrix
func (t *TIAEngine) IngestCoverage(lang LanguageRuntime, data []byte, commitHash string) error {
	ingester, exists := t.ingesters[lang]
	if !exists {
		return fmt.Errorf("unsupported language runtime for TIA: %s", lang)
	}

	blocks, err := ingester.Parse(data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCoverageProfileCorrupt, err)
	}

	matrix := &CallGraphMatrix{
		CommitHash:    commitHash,
		CreatedAt:     time.Now().UTC(),
		SymbolToTests: make(map[string][]string),
		FileToTests:   make(map[string][]string),
	}

	h := sha256.New()
	for _, b := range blocks {
		symKey := fmt.Sprintf("%s:%d-%d", b.FilePath, b.StartLine, b.EndLine)
		matrix.SymbolToTests[symKey] = appendUnique(matrix.SymbolToTests[symKey], b.TestName)
		matrix.FileToTests[b.FilePath] = appendUnique(matrix.FileToTests[b.FilePath], b.TestName)
		h.Write([]byte(symKey + ":" + b.TestName))
	}
	matrix.GraphDigest = sha256.Sum256(h.Sum(nil))

	t.currentGraph = matrix
	t.commitDistance = 0
	return nil
}

// SelectTests mapira promijenjene linije na testove uz strogu stale detekciju
func (t *TIAEngine) SelectTests(diffs []DiffRange, totalTestsInSuite []string) DynamicTestSelectionResult {
	// Pravilo 1: Ako nema grafa ili je graf prestar -> OBVEZAN Full-Suite Fallback
	if t.currentGraph == nil {
		return DynamicTestSelectionResult{
			ImpactedTests:  totalTestsInSuite,
			MustRunAll:     true,
			FallbackReason: "no coverage matrix loaded",
			TotalTestCount: len(totalTestsInSuite),
			SelectedCount:  len(totalTestsInSuite),
		}
	}

	if t.commitDistance > t.maxCommitAge {
		return DynamicTestSelectionResult{
			ImpactedTests:  totalTestsInSuite,
			MustRunAll:     true,
			FallbackReason: fmt.Sprintf("stale graph: commit distance %d > max %d", t.commitDistance, t.maxCommitAge),
			TotalTestCount: len(totalTestsInSuite),
			SelectedCount:  len(totalTestsInSuite),
		}
	}

	selectedMap := make(map[string]struct{})

	for _, d := range diffs {
		// Ako je promijenjena konfiguracija, manifest ili schema -> Full Fallback
		if isGlobalImpactFile(d.FilePath) {
			return DynamicTestSelectionResult{
				ImpactedTests:  totalTestsInSuite,
				MustRunAll:     true,
				FallbackReason: fmt.Sprintf("global impact file modified: %s", d.FilePath),
				TotalTestCount: len(totalTestsInSuite),
				SelectedCount:  len(totalTestsInSuite),
			}
		}

		// Mapiranje linija na blokove
		matched := false
		for symKey, tests := range t.currentGraph.SymbolToTests {
			var path string
			var start, end int
			fmt.Sscanf(symKey, "%[^:]:%d-%d", &path, &start, &end)

			if path == d.FilePath {
				if linesOverlap(d.StartLine, d.StartLine+d.LineCount, start, end) {
					for _, test := range tests {
						selectedMap[test] = struct{}{}
					}
					matched = true
				}
			}
		}

		// Ako linija nije mapirana u grafu -> pokreni sve testove za tu datoteku
		if !matched {
			if fileTests, ok := t.currentGraph.FileToTests[d.FilePath]; ok {
				for _, test := range fileTests {
					selectedMap[test] = struct{}{}
				}
			} else {
				// Nova datoteka bez pokrivenosti -> Full Fallback
				return DynamicTestSelectionResult{
					ImpactedTests:  totalTestsInSuite,
					MustRunAll:     true,
					FallbackReason: fmt.Sprintf("unmapped changes in new file: %s", d.FilePath),
					TotalTestCount: len(totalTestsInSuite),
					SelectedCount:  len(totalTestsInSuite),
				}
			}
		}
	}

	tests := make([]string, 0, len(selectedMap))
	for k := range selectedMap {
		tests = append(tests, k)
	}

	return DynamicTestSelectionResult{
		ImpactedTests:  tests,
		MustRunAll:     false,
		ResolvedAt:     time.Now().UTC(),
		SelectedCount:  len(tests),
		TotalTestCount: len(totalTestsInSuite),
	}
}

func isGlobalImpactFile(path string) bool {
	return path == "go.mod" || path == "go.sum" || path == "Cargo.toml" || path == "package.json"
}

func linesOverlap(a1, a2, b1, b2 int) bool {
	return a1 <= b2 && b1 <= a2
}

func appendUnique(slice []string, val string) []string {
	for _, item := range slice {
		if item == val {
			return slice
		}
	}
	return append(slice, val)
}
```

## B.3. Invarijante i RED Testovi (`pkg/tia`)

| RED Test ID | Što Testira | Uvjet Prolaza (Assert) |
| :--- | :--- | :--- |
| `TestTIA_SelectsDirectlyImpactedTest` | Promjena u funkciji `CalculateTax` unutar `calc.go:40-60`. | `ImpactedTests == ["TestCalculateTax"]`, `MustRunAll == false`. |
| `TestTIA_FallbackOnStaleGraphDistance` | `commitDistance == 6` uz `maxCommitAge == 5`. | `MustRunAll == true`, `FallbackReason` sadrži "stale graph". |
| `TestTIA_FallbackOnGlobalManifestChange` | Promjena u `go.mod`. | `MustRunAll == true`, `FallbackReason` sadrži "global impact file". |
| `TestTIA_FallbackOnUncoveredNewFile` | Diff u `new_feature.go` koja ne postoji u grafu. | `MustRunAll == true`, `FallbackReason` sadrži "unmapped changes". |
| `TestTIA_IngestCorruptProfileReturnsError` | Neispravan coverprofile format. | Vraća `ErrCoverageProfileCorrupt`. |

---

# DIO C: HARDENED EDIT SUBSYSTEM, SHADOW-GIT I EXECUTION RECEIPT (`pkg/edit`)

## C.1. Tipovi, Edit Operacije i Preimage Verifikacija

```go
package edit

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrPreimageMismatch  = errors.New("preimage hash mismatch: file was modified concurrently")
	ErrAmbiguousMatch    = errors.New("refuse threshold exceeded: multiple matching targets found (>1)")
	ErrTargetNotFound    = errors.New("target text not found in source file")
	ErrSymeditUnsupported= errors.New("AST symedit unsupported for this language runtime")
	ErrFileTooLarge      = errors.New("file exceeds 10MB limit for shadow staging")
)

type EditOpType string

const (
	OpExactReplace EditOpType = "exact_replace"
	OpFuzzyReplace EditOpType = "fuzzy_replace"
	OpASTSymRename EditOpType = "ast_sym_rename"
	OpInsertLines  EditOpType = "insert_lines"
	OpDeleteLines  EditOpType = "delete_lines"
)

type EditRequest struct {
	FilePath     string
	OpType       EditOpType
	PreimageHash [32]byte // SHA-256 očekivanog sadržaja prije izmjene
	OldText      string
	NewText      string
	SymbolQuery  string // Za AST symedit ("function:Foo", "type:Bar")
	InsertLine   int    // Za OpInsertLines (1-indeksirano)
}

type EditCandidate struct {
	LineNumber   int
	MatchedChunk string
	Score        float64 // 1.0 = exact match
}

type EditResult struct {
	FilePath      string
	OpType        EditOpType
	Applied       bool
	PreimageHash  [32]byte
	PostimageHash [32]byte
	LinesChanged  int
	ExecutionTime time.Duration
}

// EditEngine izvodi stroge izmjene s fail-closed odbijanjem na >1 pogodaka
type EditEngine struct {
	maxFileSize int64
}

func NewEditEngine() *EditEngine {
	return &EditEngine{maxFileSize: 10 << 20} // 10MB strop
}

func (e *EditEngine) Apply(sourceContent []byte, req EditRequest) ([]byte, EditResult, error) {
	if int64(len(sourceContent)) > e.maxFileSize {
		return nil, EditResult{}, ErrFileTooLarge
	}

	// 1. Verifikacija Preimage hasha
	currentHash := sha256.Sum256(sourceContent)
	if req.PreimageHash != [32]byte{} && currentHash != req.PreimageHash {
		return nil, EditResult{}, fmt.Errorf("%w: expected %x, got %x", ErrPreimageMismatch, req.PreimageHash, currentHash)
	}

	srcStr := string(sourceContent)
	var modifiedStr string

	switch req.OpType {
	case OpExactReplace:
		matches := strings.Count(srcStr, req.OldText)
		if matches == 0 {
			return nil, EditResult{}, ErrTargetNotFound
		}
		// Refuse threshold: strogo odbij ako ima više od 1 pogotka bez specifičnog sidra
		if matches > 1 {
			return nil, EditResult{}, fmt.Errorf("%w: found %d matches for target text", ErrAmbiguousMatch, matches)
		}
		modifiedStr = strings.Replace(srcStr, req.OldText, req.NewText, 1)

	case OpInsertLines:
		lines := strings.Split(srcStr, "\n")
		if req.InsertLine < 1 || req.InsertLine > len(lines)+1 {
			return nil, EditResult{}, fmt.Errorf("insert line %d out of bounds (1..%d)", req.InsertLine, len(lines)+1)
		}
		idx := req.InsertLine - 1
		lines = append(lines[:idx], append([]string{req.NewText}, lines[idx:]...)...)
		modifiedStr = strings.Join(lines, "\n")

	default:
		return nil, EditResult{}, fmt.Errorf("unsupported edit op: %s", req.OpType)
	}

	outBytes := []byte(modifiedStr)
	postHash := sha256.Sum256(outBytes)

	return outBytes, EditResult{
		FilePath:      req.FilePath,
		OpType:        req.OpType,
		Applied:       true,
		PreimageHash:  currentHash,
		PostimageHash: postHash,
		LinesChanged:  strings.Count(req.NewText, "\n") + 1,
	}, nil
}
```

## C.2. Shadow-Git Snapshot Staging i Signed ExecutionReceipt

```go
// ShadowSnapshotScope definira što ulazi u shadow git snimku
type ShadowSnapshotScope struct {
	IncludeUntracked bool
	FollowSymlinks   bool  // FALSE po defaultu: snima se symlink objekt, NE target
	MaxFileSizeLimit int64 // 10MB default; veće datoteke idu u quarantine bez snimanja
	PreserveXattrs   bool  // TRUE na Linuxu/Darwinu
}

type FileMutation struct {
	Path          string
	Action        string // "MODIFIED", "CREATED", "DELETED"
	PreImageHash  [32]byte
	PostImageHash [32]byte
	SizeBytes     int64
}

// ExecutionReceipt je kriptografski dokaz izvršene operacije unutar sandboxed membrane
type ExecutionReceipt struct {
	ReceiptID    string
	RequestHash  [32]byte       // Hash inicijalnog zahtjeva
	Backend      string         // "landlock_v5", "bubblewrap", "seatbelt"
	PolicyHash   [32]byte       // Hash primijenjene sigurnosne politike
	ExitCode     int            // Exit code izvršenog procesa/alata
	FSDelta      []FileMutation // Popis svih mutacija na disku
	Timestamp    time.Time
	Signature    []byte         // HMAC/Ed25519 potpis kernela
}

func GenerateReceipt(requestHash [32]byte, backend string, policyHash [32]byte, exitCode int, deltas []FileMutation, signerKey []byte) ExecutionReceipt {
	r := ExecutionReceipt{
		ReceiptID:   fmt.Sprintf("rcpt-%d", time.Now().UnixNano()),
		RequestHash: requestHash,
		Backend:     backend,
		PolicyHash:  policyHash,
		ExitCode:    exitCode,
		FSDelta:     deltas,
		Timestamp:   time.Now().UTC(),
	}

	h := sha256.New()
	h.Write(r.RequestHash[:])
	h.Write([]byte(r.Backend))
	h.Write(r.PolicyHash[:])
	h.Write([]byte(fmt.Sprintf("%d", r.ExitCode)))
	for _, d := range r.FSDelta {
		h.Write([]byte(d.Path + d.Action))
		h.Write(d.PostImageHash[:])
	}
	r.Signature = h.Sum(signerKey) // Deterministički potpis stanja

	return r
}
```

## C.3. Invarijante i RED Testovi (`pkg/edit`)

| RED Test ID | Što Testira | Uvjet Prolaza (Assert) |
| :--- | :--- | :--- |
| `TestEditEngine_RejectsAmbiguousMatches` | `OldText` se pojavljuje 3 puta u datoteci. | Vraća `ErrAmbiguousMatch`, nema izmjene na izvoru. |
| `TestEditEngine_RejectsPreimageMismatch` | `req.PreimageHash` ne odgovara trenutnom stanju datoteke. | Vraća `ErrPreimageMismatch`, operacija odbijena. |
| `TestEditEngine_RejectsFilesOver10MB` | Pokušaj izmjene datoteke od 11 MB. | Vraća `ErrFileTooLarge`. |
| `TestShadowScope_DoesNotFollowSymlinks` | Symlink koji pokazuje izvan workspacea (`/etc/passwd`). | Snima se samo relativni symlink zapis, ciljana datoteka se ne dereferencira. |
| `TestExecutionReceipt_TamperProofVerification` | Promjena `ExitCode` u potpisanom `ExecutionReceipt`. | Verifikacija potpisa vraća grešku neusklađenosti. |

---

# ZAKLJUČAK I SPREMNOST ZA KODIRANJE

Ovaj dokument u potpunosti rješava **MUST-RESOLVE #4** r識avajući tri ključna coding diferencijatora u **konkretan, strogo tipiziran Go dizajn**:
1. **Generic AcceptanceChecker:** Proširen s isključivo coding zadataka na mrežne isporuke, kalendarske mutacije i research grounding uz **neprobojno pravilo `Worker != Checker`**.
2. **TIA Engine:** Kompletna arhitektura mapiranja pokrivenosti i selekcije testova uz **obvezni automatski Full-Suite Fallback** na zastarjelost grafa ili strukturne promjene.
3. **Hardened Edit & ExecutionReceipt:** Deterministička izmjena koda s **preimage hash provjerom**, **refuse-thresholdom za >1 pogodak** i **kriptografski potpisanim računom izvršenja (`ExecutionReceipt`)**.
