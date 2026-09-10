// discover.go is a small pre-pass used only to discover which files a
// raw WorkspaceEdit touches, before the caller has read any preimages —
// resolving the chicken-and-egg ParseWorkspaceEdit itself creates by
// requiring preimages for every touched file up front (deliberately;
// see edit.go's own doc comment). ExtractTouchedRelPaths is a LOOSE,
// non-authoritative scan: ParseWorkspaceEdit independently re-parses
// and re-validates the SAME raw bytes afterward and fails closed on
// anything this function got wrong, missed, or a malicious/malformed
// response smuggled past it — a wrong or incomplete result here can
// never cause an incorrect edit to be accepted, only an error from
// ParseWorkspaceEdit's own strict pass.
package symedit

import (
	"encoding/json"
	"fmt"
)

// rawTouchedScan mirrors just enough of rawWorkspaceEdit/rawTextDocumentEdit
// (edit.go) to extract each entry's URI — deliberately NOT strictUnmarshal
// and NOT the full rawTextDocumentEdit shape: this pass does not need to
// (and must not pretend to) validate anything, only enumerate.
type rawTouchedScan struct {
	DocumentChanges []struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	} `json:"documentChanges"`
}

// ExtractTouchedRelPaths returns the set of workspace-relative paths raw's
// documentChanges entries name, resolved against workspaceRoot via the
// same resolveFileURI ParseWorkspaceEdit itself uses. A resource
// operation entry (no textDocument.uri — e.g. a create/rename/delete)
// yields an empty URI here, which resolveFileURI refuses; the whole call
// fails closed rather than silently skipping it (ParseWorkspaceEdit
// would refuse the same entry outright anyway, via its own kind check —
// this just surfaces the same "unsupported shape" refusal earlier).
func ExtractTouchedRelPaths(raw json.RawMessage, workspaceRoot string) ([]string, error) {
	var scan rawTouchedScan
	if err := json.Unmarshal(raw, &scan); err != nil {
		return nil, fmt.Errorf("symedit: extract touched paths: malformed WorkspaceEdit: %w", err)
	}
	if len(scan.DocumentChanges) == 0 {
		return nil, fmt.Errorf("%w: no documentChanges present", ErrUnsupportedEditShape)
	}
	seen := make(map[string]bool, len(scan.DocumentChanges))
	out := make([]string, 0, len(scan.DocumentChanges))
	for i, dc := range scan.DocumentChanges {
		relPath, err := resolveFileURI(dc.TextDocument.URI, workspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("symedit: extract touched paths: documentChanges[%d]: %w", i, err)
		}
		if seen[relPath] {
			continue
		}
		seen[relPath] = true
		out = append(out, relPath)
	}
	return out, nil
}
