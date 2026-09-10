// Package symedit is Slice 3's AST-aware rename orchestrator
// (PLAN-CODING-TRIO.md): v1 supports only RenameSymbol, driven by Slice
// 0's governed gopls stdio session (runner.RunGoplsRename).
//
// This file is the PURE layer: parsing gopls' raw `WorkspaceEdit`
// response into the closed, explicit subset Slice 3 v1 accepts, and
// applying a validated edit set to an immutable preimage to produce the
// final bytes. No disk I/O, no syscalls, no S7 — every function here is
// a plain, deterministic transformation over byte slices and JSON,
// independently testable without a sandbox or a real gopls process.
// internal/coding/workspace (not built yet) owns turning this package's
// output into a durable, crash-recoverable multi-file write.
package symedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// strictUnmarshal decodes data into v, refusing any JSON object member v's
// struct tags don't declare — recursively, through the ENTIRE nested
// struct tree in one Decode call (code-review finding, codex, round 2:
// plain json.Unmarshal silently ignores unknown fields anywhere in the
// structure, contradicting this package's own "closed, explicit subset"
// design — a future or malformed WorkspaceEdit variant carrying an
// unaccounted-for member must be refused, not silently stripped of
// meaning it may have carried).
func strictUnmarshal(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ErrUnsupportedEditShape is returned whenever gopls' response falls
// outside the narrow, explicit subset Slice 3 v1 accepts (plan-decided,
// not deferred): `documentChanges` containing ONLY `TextDocumentEdit`
// entries (no resource create/rename/delete operations, no annotations),
// local `file:` URIs with an empty authority only. A plain `changes` map
// (LSP's OTHER WorkspaceEdit shape) is never accepted either — verified
// live against real gopls (v0.23.0): it unconditionally returns
// `documentChanges`, never `changes`, regardless of advertised client
// capabilities, so `changes`-only support would never actually be
// exercised and silently masks a real gopls response this package cannot
// safely handle.
var ErrUnsupportedEditShape = errors.New("symedit: unsupported edit shape")

// FileEdit is one file's validated, byte-resolved edit set — the parsed
// and validated form of one `documentChanges` `TextDocumentEdit` entry.
type FileEdit struct {
	URI     string // the original file:// URI, kept for provenance
	RelPath string // decoded, slash-separated, workspace-relative, cleaned
	Version int64  // the LSP document version this edit set targets (validated, see ParseWorkspaceEdit)
	Edits   []TextEdit
}

// TextEdit is one validated replacement, in byte offsets resolved
// against the file's IMMUTABLE preimage (never a progressively-mutated
// copy — invariant 3).
type TextEdit struct {
	StartByte int
	EndByte   int
	NewText   string
}

// rawWorkspaceEdit, rawDocumentChangeKind, rawTextDocumentEdit,
// rawTextEdit, rawRange, rawPosition mirror the LSP wire shapes this
// package decodes. Unexported: callers only see the validated FileEdit/
// TextEdit types above.
//
// Every LSP-REQUIRED field below is a POINTER (code-review finding,
// codex, round 1: a plain non-pointer field makes "key absent from the
// JSON" and "key present with the zero value" indistinguishable to
// encoding/json — e.g. {"newText":"X"} with no "range" key at all would
// silently decode to an in-bounds insertion at byte (0,0) instead of
// being refused as malformed). A nil pointer after decode is always
// treated as "required field missing" and refused.
type rawWorkspaceEdit struct {
	Changes           map[string]json.RawMessage `json:"changes,omitempty"`
	DocumentChanges   []json.RawMessage          `json:"documentChanges,omitempty"`
	ChangeAnnotations map[string]json.RawMessage `json:"changeAnnotations,omitempty"`
}

type rawDocumentChangeKind struct {
	Kind string `json:"kind,omitempty"` // present only on a resource operation (create/rename/delete)
}

type rawTextDocumentEdit struct {
	TextDocument rawVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []rawTextEdit                      `json:"edits"`
}

type rawVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int64 `json:"version"`
}

type rawTextEdit struct {
	Range        *rawRange `json:"range"`
	NewText      *string   `json:"newText"`
	AnnotationID json.RawMessage `json:"annotationId,omitempty"` // ANY presence (including explicit null) => AnnotatedTextEdit, unsupported in v1
}

type rawRange struct {
	Start *rawPosition `json:"start"`
	End   *rawPosition `json:"end"`
}

type rawPosition struct {
	Line      *uint32 `json:"line"`
	Character *uint32 `json:"character"` // UTF-16 code unit offset within the line
}

// ParseWorkspaceEdit decodes gopls' raw rename response (runner.
// RenameResult.Result) into the closed, explicit v1 subset this package
// accepts, resolving every edit's LSP (line, UTF-16 character) position
// into a byte offset against preimages — the EXACT bytes the caller read
// from disk before asking gopls to rename (never re-fetched, never
// progressively mutated). preimages is keyed by the SAME relPath this
// function computes from each entry's URI, so the caller must supply
// every file the response could plausibly touch; a URI this function
// resolves to a relPath absent from preimages is refused (fail closed —
// invariant 5, never guess at content it was not given).
//
// openedFileRelPath is the ONE file the caller's gopls session actually
// `didOpen`'d (runner.RunGoplsRename opens exactly one file per rename
// request) — required to validate each entry's document version
// (code-review finding, codex, round 1: version was previously accepted
// unchecked). Verified live against real gopls (v0.23.0), twice: the
// explicitly-opened file's TextDocumentEdit always carries version 1
// (matching gopls.go's own hardcoded `didOpen` version); every OTHER
// file gopls touches via a cross-file rename — read from disk, never
// opened — always carries version 0. Any other value, or a missing
// version, is refused.
//
// Fails closed (wrapping ErrUnsupportedEditShape or a plain error) on:
// a `changes`-only response (documentChanges absent) or one carrying a
// non-empty `changes` map or `changeAnnotations` ALONGSIDE documentChanges
// (an edit channel this package does not process must never be silently
// dropped); any documentChanges entry that is a resource operation (has
// a "kind" field) rather than a plain TextDocumentEdit; any TextEdit
// carrying an `annotationId` (an AnnotatedTextEdit, unsupported in v1);
// a non-`file:` URI or one with a non-empty authority; a URI that
// decodes outside workspaceRoot; two entries resolving to the same
// relPath (ambiguous ordering, never merged); a missing or mismatched
// document version; any LSP-required field (uri, edits, range, start,
// end, line, character, newText) absent from the JSON — never treated
// as its Go zero value; malformed JSON at any level; any edit whose
// resolved range is out-of-range, inverted, overlapping, or duplicate
// within its own file.
func ParseWorkspaceEdit(raw json.RawMessage, workspaceRoot, openedFileRelPath string, preimages map[string][]byte) ([]FileEdit, error) {
	var wf rawWorkspaceEdit
	if err := strictUnmarshal(raw, &wf); err != nil {
		return nil, fmt.Errorf("symedit: malformed WorkspaceEdit: %w", err)
	}
	if len(wf.DocumentChanges) == 0 {
		return nil, fmt.Errorf("%w: no documentChanges present (a plain 'changes' map is never accepted)", ErrUnsupportedEditShape)
	}
	if len(wf.Changes) > 0 {
		return nil, fmt.Errorf("%w: response carries a non-empty 'changes' map alongside documentChanges (never silently dropped)", ErrUnsupportedEditShape)
	}
	if len(wf.ChangeAnnotations) > 0 {
		return nil, fmt.Errorf("%w: response carries changeAnnotations (annotations unsupported in v1)", ErrUnsupportedEditShape)
	}
	// openedFileRelPath itself must be validated, not merely compared
	// against (code-review finding, codex, round 2): an empty or
	// caller-mistyped path would silently degrade version validation to
	// "every file must be version 0," never actually confirming the ONE
	// file the gopls session opened came back at version 1 as it always
	// must. preimages[openedFileRelPath] absent means the caller itself
	// never read that file — an internal contract violation, not a
	// property of gopls' response, so it fails closed here rather than
	// silently.
	if openedFileRelPath == "" {
		return nil, fmt.Errorf("symedit: openedFileRelPath is required (fail closed)")
	}
	if _, ok := preimages[openedFileRelPath]; !ok {
		return nil, fmt.Errorf("symedit: openedFileRelPath %q has no supplied preimage (fail closed)", openedFileRelPath)
	}

	seenRelPath := map[string]bool{}
	var out []FileEdit
	for i, entry := range wf.DocumentChanges {
		var kindProbe rawDocumentChangeKind
		if err := json.Unmarshal(entry, &kindProbe); err != nil {
			return nil, fmt.Errorf("symedit: malformed documentChanges[%d]: %w", i, err)
		}
		if kindProbe.Kind != "" {
			return nil, fmt.Errorf("%w: documentChanges[%d] is a resource operation (kind=%q)", ErrUnsupportedEditShape, i, kindProbe.Kind)
		}
		var tde rawTextDocumentEdit
		if err := strictUnmarshal(entry, &tde); err != nil {
			return nil, fmt.Errorf("symedit: malformed documentChanges[%d] as TextDocumentEdit: %w", i, err)
		}
		if tde.TextDocument.URI == "" || len(tde.Edits) == 0 {
			return nil, fmt.Errorf("%w: documentChanges[%d] missing textDocument.uri or edits", ErrUnsupportedEditShape, i)
		}
		if tde.TextDocument.Version == nil {
			return nil, fmt.Errorf("%w: documentChanges[%d] missing textDocument.version", ErrUnsupportedEditShape, i)
		}

		relPath, err := resolveFileURI(tde.TextDocument.URI, workspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("symedit: documentChanges[%d]: %w", i, err)
		}
		if seenRelPath[relPath] {
			return nil, fmt.Errorf("symedit: %s targeted by more than one documentChanges entry (ambiguous ordering, refusing)", relPath)
		}
		seenRelPath[relPath] = true

		wantVersion := int64(0)
		if relPath == openedFileRelPath {
			wantVersion = 1
		}
		if *tde.TextDocument.Version != wantVersion {
			return nil, fmt.Errorf("symedit: %s has document version %d, want %d (fail closed)", relPath, *tde.TextDocument.Version, wantVersion)
		}

		preimage, ok := preimages[relPath]
		if !ok {
			return nil, fmt.Errorf("symedit: %s has no supplied preimage (fail closed)", relPath)
		}

		edits, err := resolveTextEdits(preimage, tde.Edits)
		if err != nil {
			return nil, fmt.Errorf("symedit: %s: %w", relPath, err)
		}

		out = append(out, FileEdit{
			URI:     tde.TextDocument.URI,
			RelPath: relPath,
			Version: *tde.TextDocument.Version,
			Edits:   edits,
		})
	}
	if !seenRelPath[openedFileRelPath] {
		// The opened file never appearing at all is otherwise
		// indistinguishable from a legitimate zero-edit response for it
		// (code-review finding, codex, round 2) — the symbol's own
		// declaration lives in the opened file, so a real rename always
		// touches it; require that explicitly rather than trust an
		// absence.
		return nil, fmt.Errorf("symedit: openedFileRelPath %q never appeared in the response (fail closed)", openedFileRelPath)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// resolveFileURI decodes a `file:` URI (empty authority only, per the
// plan's decided v1 subset) into a workspace-relative, slash-separated,
// cleaned path — refusing anything that escapes workspaceRoot.
func resolveFileURI(raw, workspaceRoot string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: malformed URI %q: %v", ErrUnsupportedEditShape, raw, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("%w: non-file URI scheme %q", ErrUnsupportedEditShape, u.Scheme)
	}
	if u.Host != "" {
		return "", fmt.Errorf("%w: URI has a non-empty authority %q", ErrUnsupportedEditShape, u.Host)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		// A real gopls file: URI never carries either (round-3 review
		// note, codex+agy independently, non-blocking): rejecting keeps
		// the "closed, explicit subset" promise literal rather than
		// silently ignoring components this package never inspects.
		return "", fmt.Errorf("%w: file URI carries a query or fragment %q", ErrUnsupportedEditShape, raw)
	}
	absPath := u.Path
	root := path.Clean("/" + filepathToSlash(workspaceRoot))
	cleanAbs := path.Clean(absPath)
	// A genuine subdirectory match requires root followed by a "/" —
	// a bare string-prefix check would wrongly accept a SIBLING whose
	// name merely starts with root's own name (e.g. "/work/src2" against
	// root "/work/src"). root=="/" is its own separator already (code
	// review note, agy) — "//" would wrongly refuse every real path.
	rootPrefix := root
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if !strings.HasPrefix(cleanAbs, rootPrefix) {
		return "", fmt.Errorf("%w: URI path %q escapes or does not resolve under workspace root %q", ErrUnsupportedEditShape, absPath, workspaceRoot)
	}
	rel := strings.TrimPrefix(cleanAbs, rootPrefix)
	if rel == "" || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", fmt.Errorf("%w: URI path %q escapes or does not resolve under workspace root %q", ErrUnsupportedEditShape, absPath, workspaceRoot)
	}
	return rel, nil
}

func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// resolveTextEdits converts every raw LSP edit's (line, UTF-16
// character) range into a byte-offset TextEdit against preimage, then
// validates the resulting set: no inverted, out-of-range, overlapping,
// or duplicate-start ranges.
func resolveTextEdits(preimage []byte, raw []rawTextEdit) ([]TextEdit, error) {
	edits := make([]TextEdit, 0, len(raw))
	for i, r := range raw {
		if r.AnnotationID != nil {
			return nil, fmt.Errorf("%w: edit[%d] is an AnnotatedTextEdit (annotationId present, unsupported in v1)", ErrUnsupportedEditShape, i)
		}
		if r.Range == nil {
			return nil, fmt.Errorf("%w: edit[%d] missing range", ErrUnsupportedEditShape, i)
		}
		if r.Range.Start == nil || r.Range.End == nil {
			return nil, fmt.Errorf("%w: edit[%d] missing range.start or range.end", ErrUnsupportedEditShape, i)
		}
		if r.NewText == nil {
			return nil, fmt.Errorf("%w: edit[%d] missing newText", ErrUnsupportedEditShape, i)
		}
		start, err := positionToByteOffset(preimage, *r.Range.Start)
		if err != nil {
			return nil, fmt.Errorf("edit[%d] start: %w", i, err)
		}
		end, err := positionToByteOffset(preimage, *r.Range.End)
		if err != nil {
			return nil, fmt.Errorf("edit[%d] end: %w", i, err)
		}
		if end < start {
			return nil, fmt.Errorf("edit[%d] has an inverted range (end %d before start %d)", i, end, start)
		}
		edits = append(edits, TextEdit{StartByte: start, EndByte: end, NewText: *r.NewText})
	}
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].StartByte != edits[j].StartByte {
			return edits[i].StartByte < edits[j].StartByte
		}
		return edits[i].EndByte < edits[j].EndByte
	})
	for i := 1; i < len(edits); i++ {
		if edits[i].StartByte < edits[i-1].EndByte || edits[i].StartByte == edits[i-1].StartByte {
			return nil, fmt.Errorf("overlapping or duplicate edit ranges (byte %d)", edits[i].StartByte)
		}
	}
	return edits, nil
}

// positionToByteOffset resolves one LSP (line, UTF-16 character)
// position into a byte offset within content. Line terminators are
// recognized as \n, \r\n, or \r (LSP's own line-splitting rule) but
// NEVER stripped or normalized in the returned offset math — content
// itself is never touched here, only walked.
func positionToByteOffset(content []byte, pos rawPosition) (int, error) {
	if pos.Line == nil || pos.Character == nil {
		return 0, fmt.Errorf("%w: position missing line or character", ErrUnsupportedEditShape)
	}
	lineStart, lineEnd, ok := nthLineBounds(content, *pos.Line)
	if !ok {
		return 0, fmt.Errorf("line %d out of range", *pos.Line)
	}
	colOffset, err := utf16ColumnToByteOffset(content[lineStart:lineEnd], *pos.Character)
	if err != nil {
		return 0, fmt.Errorf("line %d: %w", *pos.Line, err)
	}
	return lineStart + colOffset, nil
}

// nthLineBounds returns the [start,end) byte range of the given
// 0-indexed logical line's CONTENT (excluding its terminator). Handles
// \n, \r\n, and bare \r terminators without altering the buffer.
func nthLineBounds(content []byte, n uint32) (start, end int, ok bool) {
	line := uint32(0)
	pos := 0
	lineStart := 0
	for pos < len(content) {
		c := content[pos]
		if c == '\n' {
			if line == n {
				return lineStart, pos, true
			}
			pos++
			lineStart = pos
			line++
			continue
		}
		if c == '\r' {
			lineEndHere := pos
			pos++
			if pos < len(content) && content[pos] == '\n' {
				pos++
			}
			if line == n {
				return lineStart, lineEndHere, true
			}
			lineStart = pos
			line++
			continue
		}
		pos++
	}
	// Final line (no trailing terminator, or n is exactly the line after
	// the last terminator).
	if line == n {
		return lineStart, len(content), true
	}
	return 0, 0, false
}

// utf16ColumnToByteOffset walks line's UTF-8 runes, counting UTF-16 code
// units per LSP's own column semantics (a rune outside the Basic
// Multilingual Plane consumes TWO UTF-16 code units — a surrogate pair —
// not one), and returns the UTF-8 byte offset at which the requested
// UTF-16 column falls. Refuses (fail closed) a column that lands in the
// MIDDLE of a surrogate pair, or beyond the line's own UTF-16 length —
// both are invalid LSP positions, never silently clamped.
func utf16ColumnToByteOffset(line []byte, character uint32) (int, error) {
	if character == 0 {
		return 0, nil
	}
	var utf16Count uint32
	byteOffset := 0
	for byteOffset < len(line) {
		r, size := utf8.DecodeRune(line[byteOffset:])
		if r == utf8.RuneError && size <= 1 {
			return 0, fmt.Errorf("invalid UTF-8 at byte %d", byteOffset)
		}
		units := uint32(1)
		if r > 0xFFFF {
			units = 2 // astral-plane rune: encoded as a UTF-16 surrogate pair
		}
		if utf16Count+units > character {
			// The requested column falls strictly inside this rune's
			// UTF-16 encoding (only possible when units==2 and character
			// == utf16Count+1) — an LSP position must never reference
			// the middle of a surrogate pair.
			return 0, fmt.Errorf("UTF-16 column %d falls inside a surrogate pair at byte %d", character, byteOffset)
		}
		utf16Count += units
		byteOffset += size
		if utf16Count == character {
			return byteOffset, nil
		}
	}
	if utf16Count == character {
		return byteOffset, nil
	}
	return 0, fmt.Errorf("UTF-16 column %d beyond line length (%d UTF-16 units)", character, utf16Count)
}

// ApplyEdits produces the final bytes for one file: preimage with every
// validated, non-overlapping edit spliced in. Building a fresh buffer in
// ascending StartByte order (rather than in-place descending splicing)
// yields BYTE-IDENTICAL output — descending order only matters for an
// in-place mutation strategy, which this package deliberately avoids
// (preimage is never mutated, matching invariant 3's "immutable
// preimage" requirement). edits MUST already be validated (sorted,
// non-overlapping, in-range) — callers get that guarantee from
// ParseWorkspaceEdit; ApplyEdits itself re-validates range bounds against
// THIS preimage as a defensive fail-closed check (the preimage handed to
// Apply may differ from the one Prepare validated against — Apply-time
// digest verification is the caller's job, not this function's, but an
// out-of-range offset here must never panic via a slice-bounds crash).
func ApplyEdits(preimage []byte, edits []TextEdit) ([]byte, error) {
	sorted := make([]TextEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartByte < sorted[j].StartByte })
	for i, e := range sorted {
		if e.StartByte < 0 || e.EndByte < e.StartByte || e.EndByte > len(preimage) {
			return nil, fmt.Errorf("symedit: edit[%d] range [%d,%d) out of bounds for a %d-byte preimage", i, e.StartByte, e.EndByte, len(preimage))
		}
		if i > 0 && (e.StartByte < sorted[i-1].EndByte || e.StartByte == sorted[i-1].StartByte) {
			// The == StartByte check (code-review finding, agy) also
			// catches two duplicate zero-length insertions at the same
			// point, which e.StartByte < prev.EndByte alone misses (a
			// zero-length range [X,X) never satisfies X < X) — mirrors
			// resolveTextEdits' own stricter check above, applied here
			// too since ApplyEdits may be called directly, bypassing it.
			return nil, fmt.Errorf("symedit: edit[%d] overlaps or duplicates the previous edit's start", i)
		}
	}
	var out []byte
	cursor := 0
	for _, e := range sorted {
		out = append(out, preimage[cursor:e.StartByte]...)
		out = append(out, []byte(e.NewText)...)
		cursor = e.EndByte
	}
	out = append(out, preimage[cursor:]...)
	return out, nil
}
