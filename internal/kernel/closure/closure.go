// Package closure owns capability-flag closure resolution, S0.5-min scope
// (tasks-P0 T11; HARDQ B9): fail-closed Resolve VALIDATION (unknown, cycle,
// conflict → REJECTED) plus the SEALED startup capability snapshot built
// from config and live probe results. P0 has NO runtime activation: no
// Activator, no RollbackVault — a config change means restart. The
// transactional activation design (E7) activates with the first dynamic
// consumer (extensions, P4+).
package closure

import (
	"fmt"
	"sort"
)

// Manifest declares one capability: what it requires (other capabilities),
// what it conflicts with, and which PROBES must pass for it to switch on.
type Manifest struct {
	Name      string
	Requires  []string
	Conflicts []string
	Probes    []string // probe names that must all PASS
}

// P0Capabilities is the compiled, closed P0 set (PRD §4 + yolo F2 is a
// policy mode, not a capability).
func P0Capabilities() []Manifest {
	return []Manifest{
		{Name: "conversation", Probes: []string{"provider"}},
		{Name: "memory", Requires: []string{"conversation"}, Probes: []string{"store"}},
		{Name: "obligations", Requires: []string{"memory"}, Probes: []string{"store"}},
		{Name: "profiles", Probes: []string{"store"}},
		{Name: "telegram", Requires: []string{"conversation", "profiles"}, Probes: []string{"channel"}},
		{Name: "exec", Probes: []string{"sandbox"}},
	}
}

// Resolve validates a requested capability set against manifests:
// unknown names, dependency cycles, unknown requirements and conflicts are
// all REJECTED (fail closed). On success it returns the transitive closure
// in deterministic topological order.
func Resolve(manifests []Manifest, requested []string) ([]string, error) {
	byName := map[string]Manifest{}
	for _, m := range manifests {
		if m.Name == "" {
			return nil, fmt.Errorf("closure: manifest with empty name")
		}
		if _, dup := byName[m.Name]; dup {
			return nil, fmt.Errorf("closure: duplicate manifest %q", m.Name)
		}
		byName[m.Name] = m
	}
	// Transitive closure with cycle detection (DFS, three-color).
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var order []string
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		m, ok := byName[name]
		if !ok {
			return fmt.Errorf("closure: unknown capability %q (fail closed)", name)
		}
		switch color[name] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("closure: dependency cycle through %q (fail closed)", name)
		}
		color[name] = gray
		reqs := append([]string{}, m.Requires...)
		sort.Strings(reqs) // deterministic traversal
		for _, r := range reqs {
			if err := visit(r, append(path, name)); err != nil {
				return err
			}
		}
		color[name] = black
		order = append(order, name)
		return nil
	}
	req := append([]string{}, requested...)
	sort.Strings(req)
	for _, name := range req {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	// Conflicts checked over the RESOLVED set (a conflict pulled in
	// transitively is still a conflict).
	inSet := map[string]bool{}
	for _, n := range order {
		inSet[n] = true
	}
	for _, n := range order {
		for _, c := range byName[n].Conflicts {
			if inSet[c] {
				return nil, fmt.Errorf("closure: %q conflicts with %q (fail closed)", n, c)
			}
		}
	}
	return order, nil
}

// ProbeResult is one live probe outcome (accepted as data: the composing
// root runs real probes — sandbox/provider/channel; tests pass
// contract-valid fakes; T27 verifies the final all-live snapshot).
// ConfigHash binds the measurement to the configuration it was taken under
// (Annex P0.5: a changed config invalidates the grant — Phase-1B codex #18:
// a stale result from another config must not turn a capability ON).
type ProbeResult struct {
	Name       string
	Passed     bool
	Detail     string
	ConfigHash string
}

// CapabilityStatus is one sealed entry.
type CapabilityStatus struct {
	Name   string
	On     bool
	Reason string // OFF reason (probe/dependency), empty when On
}

// Snapshot is the SEALED startup capability state: immutable after Seal;
// changing configuration means restarting the process (B9).
type Snapshot struct {
	statuses map[string]CapabilityStatus
	order    []string
}

// Seal resolves the requested set and switches each capability ON only if
// every required probe passed UNDER THE SAME CONFIG HASH and every required
// capability is ON. Missing probe = FAILED; duplicate probe names are
// REJECTED (Phase-1B codex #19: last-write-wins let input order decide
// capability state); a probe measured under a different config hash =
// FAILED (codex #18).
func Seal(manifests []Manifest, requested []string, probes []ProbeResult, configHash string) (*Snapshot, error) {
	if configHash == "" {
		return nil, fmt.Errorf("closure: a config hash binding is required (fail closed)")
	}
	order, err := Resolve(manifests, requested)
	if err != nil {
		return nil, err
	}
	byName := map[string]Manifest{}
	for _, m := range manifests {
		byName[m.Name] = m
	}
	probeOK := map[string]ProbeResult{}
	for _, p := range probes {
		if _, dup := probeOK[p.Name]; dup {
			return nil, fmt.Errorf("closure: duplicate probe result %q (fail closed)", p.Name)
		}
		probeOK[p.Name] = p
	}
	snap := &Snapshot{statuses: map[string]CapabilityStatus{}, order: order}
	for _, name := range order { // topological: dependencies decided first
		m := byName[name]
		status := CapabilityStatus{Name: name, On: true}
		for _, probe := range m.Probes {
			pr, present := probeOK[probe]
			if !present {
				status.On = false
				status.Reason = fmt.Sprintf("probe %q missing (fail closed)", probe)
				break
			}
			if pr.ConfigHash != configHash {
				status.On = false
				status.Reason = fmt.Sprintf("probe %q measured under a different config (stale, fail closed)", probe)
				break
			}
			if !pr.Passed {
				status.On = false
				status.Reason = fmt.Sprintf("probe %q failed: %s", probe, pr.Detail)
				break
			}
		}
		if status.On {
			for _, req := range m.Requires {
				if !snap.statuses[req].On {
					status.On = false
					status.Reason = fmt.Sprintf("requires %q which is OFF", req)
					break
				}
			}
		}
		snap.statuses[name] = status
	}
	return snap, nil
}

// On reports whether a capability is active in the sealed snapshot.
// Unknown names are OFF (fail closed), never an error a caller could skip.
func (s *Snapshot) On(name string) bool {
	return s.statuses[name].On
}

// Status returns the full sealed entry (zero value for unknown names).
func (s *Snapshot) Status(name string) CapabilityStatus {
	return s.statuses[name]
}

// List returns all sealed entries in topological order.
func (s *Snapshot) List() []CapabilityStatus {
	out := make([]CapabilityStatus, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.statuses[n])
	}
	return out
}
