// Package fabricstate detects a node's InfiniBand tenant p_key layout from
// /sys/class/infiniband.
package fabricstate

// Kind is the node's tenant p_key layout.
type Kind string

const (
	// Indexed: the tenant p_key sits at the same non-zero index on every port.
	Indexed Kind = "indexed"
	// Slot0: the tenant p_key sits at index 0 (hand-made layout).
	Slot0 Kind = "slot0"
	// None: no active InfiniBand port has a tenant entry.
	None Kind = "none"
	// Ambiguous: ports disagree, a port has several tenant entries, or a
	// p_key table could not be read.
	Ambiguous Kind = "ambiguous"
)

// State is the detected layout. PKey is the tenant p_key without the 0x8000
// membership bit; Index is valid when Kind is Indexed.
type State struct {
	Kind  Kind
	PKey  uint16
	Index int
}

// Kinds lists every Kind, for code that reports per-kind output.
var Kinds = []Kind{Indexed, Slot0, None, Ambiguous}
