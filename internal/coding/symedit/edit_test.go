package symedit

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v string) json.RawMessage {
	t.Helper()
	return json.RawMessage(v)
}

// Detector: a real, minimal single-file rename — one edit, ASCII content
// — parses to the expected byte offsets and applies to the expected text.
func TestParseWorkspaceEditAndApplySingleFile(t *testing.T) {
	preimage := []byte("package leaf\n\nfunc Double(n int) int { return n * 2 }\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/leaf/leaf.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 11}}, "newText": "Triple"}
				]
			}
		]
	}`)
	edits, err := ParseWorkspaceEdit(raw, "/work/src", "leaf/leaf.go", map[string][]byte{"leaf/leaf.go": preimage})
	if err != nil {
		t.Fatalf("ParseWorkspaceEdit failed: %v", err)
	}
	if len(edits) != 1 || edits[0].RelPath != "leaf/leaf.go" {
		t.Fatalf("edits = %+v, want one entry for leaf/leaf.go", edits)
	}
	if edits[0].Version != 1 {
		t.Fatalf("Version = %v, want 1", edits[0].Version)
	}
	got, err := ApplyEdits(preimage, edits[0].Edits)
	if err != nil {
		t.Fatalf("ApplyEdits failed: %v", err)
	}
	want := "package leaf\n\nfunc Triple(n int) int { return n * 2 }\n"
	if string(got) != want {
		t.Fatalf("ApplyEdits = %q, want %q", got, want)
	}
}

// Detector: a rename touching multiple files, multiple edits within one
// file, applied in the right order (edits within a file are naturally
// non-overlapping and sorted for splicing) — produces exact results in
// EVERY file, not just the first.
func TestParseWorkspaceEditMultipleFilesMultipleEdits(t *testing.T) {
	a := []byte("package a\n\nfunc Old() {}\n\nfunc callOld() { Old() }\n")
	b := []byte("package b\n\nimport \"a\"\n\nfunc use() { a.Old() }\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/a/a.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 8}}, "newText": "New"},
					{"range": {"start": {"line": 4, "character": 17}, "end": {"line": 4, "character": 20}}, "newText": "New"}
				]
			},
			{
				"textDocument": {"uri": "file:///work/src/b/b.go", "version": 0},
				"edits": [
					{"range": {"start": {"line": 4, "character": 15}, "end": {"line": 4, "character": 18}}, "newText": "New"}
				]
			}
		]
	}`)
	edits, err := ParseWorkspaceEdit(raw, "/work/src", "a/a.go", map[string][]byte{"a/a.go": a, "b/b.go": b})
	if err != nil {
		t.Fatalf("ParseWorkspaceEdit failed: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 file edits, got %d", len(edits))
	}
	// sorted by RelPath: a/a.go, b/b.go
	gotA, err := ApplyEdits(a, edits[0].Edits)
	if err != nil {
		t.Fatal(err)
	}
	wantA := "package a\n\nfunc New() {}\n\nfunc callOld() { New() }\n"
	if string(gotA) != wantA {
		t.Fatalf("a.go = %q, want %q", gotA, wantA)
	}
	gotB, err := ApplyEdits(b, edits[1].Edits)
	if err != nil {
		t.Fatal(err)
	}
	wantB := "package b\n\nimport \"a\"\n\nfunc use() { a.New() }\n"
	if string(gotB) != wantB {
		t.Fatalf("b.go = %q, want %q", gotB, wantB)
	}
}

// Detector (plan-required: "non-BMP UTF-16 columns"): a rune outside the
// Basic Multilingual Plane (here: an emoji, U+1F600) consumes TWO UTF-16
// code units. An edit whose column is positioned AFTER such a rune must
// resolve to the correct byte offset — a naive rune-count-based (rather
// than UTF-16-unit-count-based) implementation would resolve one column
// short.
func TestParseWorkspaceEditHandlesNonBMPRune(t *testing.T) {
	// "x = 😀old" — 😀 is U+1F600 (2 UTF-16 units), "old" starts at UTF-16
	// column 4 (x,space,=,space) + 2 (the emoji) = 6.
	preimage := []byte("x = \U0001F600old\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 0, "character": 6}, "end": {"line": 0, "character": 9}}, "newText": "new"}
				]
			}
		]
	}`)
	edits, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err != nil {
		t.Fatalf("ParseWorkspaceEdit failed: %v", err)
	}
	got, err := ApplyEdits(preimage, edits[0].Edits)
	if err != nil {
		t.Fatal(err)
	}
	want := "x = \U0001F600new\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Detector: a UTF-16 column landing in the MIDDLE of a surrogate pair
// (character 5, one past the emoji's start at 4, which is not the
// boundary after the full 2-unit rune) must be refused, not silently
// resolved to some nearby byte offset.
func TestParseWorkspaceEditRefusesColumnInsideSurrogatePair(t *testing.T) {
	preimage := []byte("x = \U0001F600y\n") // emoji starts at UTF-16 column 4, ends at 6
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 0, "character": 5}, "end": {"line": 0, "character": 6}}, "newText": "z"}
				]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err == nil {
		t.Fatal("expected an error for a column inside a surrogate pair")
	}
}

// Detector (plan-required: "CRLF preserved correctly"): a file using
// CRLF line endings must have its line/character positions resolved
// against the CORRECT byte offsets (skipping the \r\n terminator, not
// just \n), and CRLF terminators elsewhere in the file must survive
// untouched in the output.
func TestParseWorkspaceEditPreservesCRLF(t *testing.T) {
	preimage := []byte("package p\r\n\r\nfunc Old() {}\r\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 8}}, "newText": "New"}
				]
			}
		]
	}`)
	edits, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err != nil {
		t.Fatalf("ParseWorkspaceEdit failed: %v", err)
	}
	got, err := ApplyEdits(preimage, edits[0].Edits)
	if err != nil {
		t.Fatal(err)
	}
	want := "package p\r\n\r\nfunc New() {}\r\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q (CRLF must survive exactly)", got, want)
	}
}

// Detector (plan-required: "overlapping/out-of-range edits rejected"):
// two edits whose byte ranges overlap must be refused.
func TestParseWorkspaceEditRejectsOverlappingRanges(t *testing.T) {
	preimage := []byte("func OldName() {}\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 0, "character": 5}, "end": {"line": 0, "character": 12}}, "newText": "A"},
					{"range": {"start": {"line": 0, "character": 8}, "end": {"line": 0, "character": 12}}, "newText": "B"}
				]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err == nil {
		t.Fatal("expected an error for overlapping edit ranges")
	}
}

// Detector: an out-of-range line (beyond the file's own line count) is
// refused.
func TestParseWorkspaceEditRejectsOutOfRangeLine(t *testing.T) {
	preimage := []byte("one line only\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [
					{"range": {"start": {"line": 5, "character": 0}, "end": {"line": 5, "character": 3}}, "newText": "x"}
				]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err == nil {
		t.Fatal("expected an error for an out-of-range line")
	}
}

// Detector (plan-decided v1 scope): a `changes`-only response (no
// documentChanges) must be refused, wrapping ErrUnsupportedEditShape —
// verified live against real gopls that this never actually happens in
// practice, but the refusal must still be explicit and typed.
func TestParseWorkspaceEditRejectsChangesOnly(t *testing.T) {
	raw := mustJSON(t, `{"changes": {"file:///work/src/f.go": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":1}}, "newText":"x"}]}}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape, got %v", err)
	}
}

// Detector (plan-decided v1 scope): a resource operation (create/rename/
// delete) inside documentChanges must be refused, wrapping
// ErrUnsupportedEditShape.
func TestParseWorkspaceEditRejectsResourceOperation(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"kind": "rename", "oldUri": "file:///work/src/old.go", "newUri": "file:///work/src/new.go"}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape, got %v", err)
	}
}

// Detector: a URI that escapes the workspace root (e.g. a symlink-style
// or absolute path pointing outside it) must be refused.
func TestParseWorkspaceEditRejectsURIEscapingWorkspaceRoot(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///etc/passwd", "version": 0},
				"edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape for an out-of-root URI, got %v", err)
	}
}

// Detector: a non-file URI scheme (e.g. an untitled buffer) must be
// refused.
func TestParseWorkspaceEditRejectsNonFileURI(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "untitled:Untitled-1", "version": 0},
				"edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape, got %v", err)
	}
}

// Detector: a documentChanges entry naming a file with no supplied
// preimage must be refused (fail closed, never guess).
func TestParseWorkspaceEditRejectsMissingPreimage(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{})
	if err == nil {
		t.Fatal("expected an error for a missing preimage")
	}
}

// Detector: two documentChanges entries targeting the SAME file must be
// refused (ambiguous ordering), never silently merged.
func TestParseWorkspaceEditRejectsDuplicateFileEntries(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"textDocument": {"uri": "file:///work/src/f.go", "version": 1}, "edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"a"}]},
			{"textDocument": {"uri": "file:///work/src/f.go", "version": 1}, "edits": [{"range": {"start":{"line":0,"character":1},"end":{"line":0,"character":1}}, "newText":"b"}]}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("xy\n")})
	if err == nil {
		t.Fatal("expected an error for duplicate file entries")
	}
}

// Detector: ApplyEdits itself refuses an out-of-bounds edit rather than
// panicking on a slice-bounds crash — defense in depth even though
// ParseWorkspaceEdit already validates against ITS OWN preimage (Apply
// may run against a DIFFERENT, re-read preimage later).
func TestApplyEditsRefusesOutOfBoundsDefensively(t *testing.T) {
	preimage := []byte("short\n")
	_, err := ApplyEdits(preimage, []TextEdit{{StartByte: 0, EndByte: 100, NewText: "x"}})
	if err == nil {
		t.Fatal("expected an error for an out-of-bounds edit")
	}
}

// Detector: a zero-length insertion (start==end) is a valid edit
// (insertion, not replacement) and applies correctly.
func TestApplyEditsHandlesZeroLengthInsertion(t *testing.T) {
	preimage := []byte("func Old() {}\n")
	got, err := ApplyEdits(preimage, []TextEdit{{StartByte: 5, EndByte: 5, NewText: "Prefix"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "func PrefixOld() {}\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUtf16ColumnToByteOffsetBasicASCII(t *testing.T) {
	off, err := utf16ColumnToByteOffset([]byte("hello world"), 6)
	if err != nil {
		t.Fatal(err)
	}
	if off != 6 {
		t.Fatalf("offset = %d, want 6", off)
	}
}

func TestResolveFileURIRejectsPrefixCollision(t *testing.T) {
	// "/work/src2/f.go" must NOT be treated as inside "/work/src" just
	// because the string "/work/src" is a textual prefix.
	_, err := resolveFileURI("file:///work/src2/f.go", "/work/src")
	if err == nil {
		t.Fatal("expected an error: /work/src2 is a sibling, not a subdirectory, of /work/src")
	}
}

func TestResolveFileURIAcceptsNestedPath(t *testing.T) {
	rel, err := resolveFileURI("file:///work/src/a/b/c.go", "/work/src")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "a/b/c.go" {
		t.Fatalf("rel = %q, want a/b/c.go", rel)
	}
}

func TestErrUnsupportedEditShapeMessageIsStable(t *testing.T) {
	if !strings.Contains(ErrUnsupportedEditShape.Error(), "unsupported edit shape") {
		t.Fatalf("unexpected error text: %s", ErrUnsupportedEditShape.Error())
	}
}

// Detector (code-review finding, codex, round 1: document version was
// accepted unchecked): the explicitly-opened file must carry version 1
// (matching gopls.go's own hardcoded didOpen version, verified live
// against real gopls); a mismatched version is refused.
func TestParseWorkspaceEditRejectsMismatchedOpenedFileVersion(t *testing.T) {
	preimage := []byte("func Old() {}\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 2},
				"edits": [{"range": {"start":{"line":0,"character":5},"end":{"line":0,"character":8}}, "newText":"New"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": preimage})
	if err == nil {
		t.Fatal("expected an error for a version mismatch on the opened file")
	}
}

// Detector (code-review finding, codex, round 1): a file gopls did NOT
// explicitly open (reached only via a cross-file rename) must carry
// version 0 — verified live against real gopls (a second, un-opened file
// in a cross-package rename response). A non-zero version on such a file
// is refused.
func TestParseWorkspaceEditRejectsNonZeroVersionOnUnopenedFile(t *testing.T) {
	preimage := []byte("func Old() {}\n")
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [{"range": {"start":{"line":0,"character":5},"end":{"line":0,"character":8}}, "newText":"New"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "other.go", map[string][]byte{"f.go": preimage})
	if err == nil {
		t.Fatal("expected an error: f.go was not the opened file, so it must carry version 0, not 1")
	}
}

// Detector (code-review finding, codex, round 1): a response carrying a
// non-empty `changes` map ALONGSIDE documentChanges must be refused, not
// silently processed via documentChanges while dropping `changes`.
func TestParseWorkspaceEditRejectsChangesAlongsideDocumentChanges(t *testing.T) {
	raw := mustJSON(t, `{
		"changes": {"file:///work/src/other.go": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]},
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape, got %v", err)
	}
}

// Detector (code-review finding, codex+agy, round 1): an AnnotatedTextEdit
// (an ordinary TextEdit plus an `annotationId`) must be refused, not
// silently accepted as a plain TextEdit with the annotation dropped.
func TestParseWorkspaceEditRejectsAnnotatedTextEdit(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{
				"textDocument": {"uri": "file:///work/src/f.go", "version": 1},
				"edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x", "annotationId": "rename-1"}]
			}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape for an AnnotatedTextEdit, got %v", err)
	}
}

// Detector (code-review finding, codex, round 1, HIGH: the most severe
// finding): a response with a required field ABSENT from the JSON
// (rather than present with the Go zero value) must be refused, never
// silently treated as if the zero value were explicitly provided.
func TestParseWorkspaceEditRejectsMissingRequiredFields(t *testing.T) {
	preimage := []byte("func Old() {}\n")
	cases := []struct {
		name string
		raw  string
	}{
		{"missing range", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"newText":"X"}]}]}`},
		{"missing range.start", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"end":{"line":0,"character":1}},"newText":"X"}]}]}`},
		{"missing range.end", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0}},"newText":"X"}]}]}`},
		{"missing newText", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]}]}`},
		{"missing line", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"character":0},"end":{"line":0,"character":1}},"newText":"X"}]}]}`},
		{"missing character", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0},"end":{"line":0,"character":1}},"newText":"X"}]}]}`},
		{"missing uri", `{"documentChanges":[{"textDocument":{"version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"X"}]}]}`},
		{"missing version", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go"},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"X"}]}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseWorkspaceEdit(mustJSON(t, c.raw), "/work/src", "f.go", map[string][]byte{"f.go": preimage})
			if err == nil {
				t.Fatalf("expected an error for %s", c.name)
			}
		})
	}
}

// Detector (code-review finding, agy, round 1): ApplyEdits called
// DIRECTLY (bypassing ParseWorkspaceEdit's stricter resolveTextEdits
// check) with two duplicate zero-length insertions at the same point
// must be refused, not silently accepted in whatever order sort.Slice
// happens to leave them.
func TestApplyEditsRejectsDuplicateZeroLengthInsertions(t *testing.T) {
	preimage := []byte("short\n")
	_, err := ApplyEdits(preimage, []TextEdit{
		{StartByte: 2, EndByte: 2, NewText: "a"},
		{StartByte: 2, EndByte: 2, NewText: "b"},
	})
	if err == nil {
		t.Fatal("expected an error for duplicate zero-length insertions at the same point")
	}
}

// Detector (code-review finding, codex, round 2): an empty
// openedFileRelPath must be refused outright, never silently degrading
// version validation to "every file must be version 0."
func TestParseWorkspaceEditRejectsEmptyOpenedFileRelPath(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"textDocument": {"uri": "file:///work/src/f.go", "version": 0}, "edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "", map[string][]byte{"f.go": []byte("x\n")})
	if err == nil {
		t.Fatal("expected an error for an empty openedFileRelPath")
	}
}

// Detector (code-review finding, codex, round 2): an openedFileRelPath
// with no supplied preimage must be refused (the caller itself never
// read that file — an internal contract violation).
func TestParseWorkspaceEditRejectsOpenedFileRelPathWithNoPreimage(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"textDocument": {"uri": "file:///work/src/f.go", "version": 1}, "edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{})
	if err == nil {
		t.Fatal("expected an error: openedFileRelPath has no supplied preimage")
	}
}

// Detector (code-review finding, codex, round 2): the opened file never
// appearing anywhere in the response must be refused — a real rename
// always touches the symbol's own declaration, which lives in the
// opened file, so its total absence is never a legitimate zero-edit
// response.
func TestParseWorkspaceEditRejectsResponseMissingOpenedFile(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"textDocument": {"uri": "file:///work/src/other.go", "version": 0}, "edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x"}]}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n"), "other.go": []byte("x\n")})
	if err == nil {
		t.Fatal("expected an error: the opened file never appeared in the response")
	}
}

// Detector (code-review finding, codex, round 2, a concrete bypass): an
// explicit "annotationId": null must be refused exactly like a real
// annotationId value — a *string field would have decoded null to nil,
// indistinguishable from an absent key; json.RawMessage correctly
// captures the key's PRESENCE regardless of its value.
func TestParseWorkspaceEditRejectsExplicitNullAnnotationID(t *testing.T) {
	raw := mustJSON(t, `{
		"documentChanges": [
			{"textDocument": {"uri": "file:///work/src/f.go", "version": 1}, "edits": [{"range": {"start":{"line":0,"character":0},"end":{"line":0,"character":0}}, "newText":"x", "annotationId": null}]}
		]
	}`)
	_, err := ParseWorkspaceEdit(raw, "/work/src", "f.go", map[string][]byte{"f.go": []byte("x\n")})
	if !errors.Is(err, ErrUnsupportedEditShape) {
		t.Fatalf("expected ErrUnsupportedEditShape for annotationId:null, got %v", err)
	}
}

// Detector (code-review finding, codex, round 2): an unknown/unexpected
// member ANYWHERE in the decoded structure — not just at the top level —
// must be refused, matching this package's "closed, explicit subset"
// design. Exercises an unknown field at three different nesting depths.
func TestParseWorkspaceEditRejectsUnknownFields(t *testing.T) {
	preimage := []byte("x\n")
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown top-level field", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x"}]}],"resultId":"abc"}`},
		{"unknown field on TextDocumentEdit", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x"}],"label":"extra"}]}`},
		{"unknown field on TextEdit", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x","snippet":true}]}]}`},
		{"unknown field on Position", `{"documentChanges":[{"textDocument":{"uri":"file:///work/src/f.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0,"extra":1},"end":{"line":0,"character":0}},"newText":"x"}]}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseWorkspaceEdit(mustJSON(t, c.raw), "/work/src", "f.go", map[string][]byte{"f.go": preimage})
			if err == nil {
				t.Fatalf("expected an error for %s", c.name)
			}
		})
	}
}

// Detector (code-review note, codex+agy round 3): a file URI carrying a
// query or fragment must be refused — real gopls never sends either, and
// this package never inspects them, so silently ignoring them would
// weaken the "closed, explicit subset" promise.
func TestResolveFileURIRejectsQueryAndFragment(t *testing.T) {
	if _, err := resolveFileURI("file:///work/src/f.go?query=1", "/work/src"); err == nil {
		t.Fatal("expected an error for a URI with a query component")
	}
	if _, err := resolveFileURI("file:///work/src/f.go#frag", "/work/src"); err == nil {
		t.Fatal("expected an error for a URI with a fragment component")
	}
}
