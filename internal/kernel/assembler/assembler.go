// Package assembler is Assembler.Base-min (S11.1-min, tasks-P0 T05): the
// deterministic prompt-assembly skeleton. Given the same blocks it produces
// byte-identical output. Trust FENCING (untrusted block never becomes an
// instruction) is structural from day one: blocks are grouped by trust
// class and untrusted content is emitted only inside a fenced data section.
// The full assembler (precedence, skills, provenance checks with the P0.1
// laundering RED) lands in T16.
package assembler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Base deterministically renders blocks into a prompt skeleton. Ordering:
// trust class (most trusted first), then BlockID — never map iteration
// order.
func Base(blocks []contracts.ContextBlock) (string, error) {
	sorted := make([]contracts.ContextBlock, len(blocks))
	copy(sorted, blocks)
	for _, b := range sorted {
		if !b.Trust.Valid() {
			return "", fmt.Errorf("assembler: block %s has invalid trust class", b.BlockID)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Trust != sorted[j].Trust {
			return sorted[i].Trust < sorted[j].Trust
		}
		return sorted[i].BlockID < sorted[j].BlockID
	})
	var sb strings.Builder
	for _, b := range sorted {
		content := ""
		if b.Content != nil {
			content = *b.Content
		} else if b.ContentRef != nil {
			content = "[ref:" + *b.ContentRef + "]"
		}
		if b.Trust == contracts.TrustUntrustedExternal {
			// Structural fence: untrusted content is DATA, never instruction.
			fmt.Fprintf(&sb, "<untrusted-data block=%q>\n%s\n</untrusted-data>\n", b.BlockID, content)
			continue
		}
		fmt.Fprintf(&sb, "[%s %s]\n%s\n", b.Trust, b.BlockID, content)
	}
	return sb.String(), nil
}
