package fabricstate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	fullMember = 0x8000
	pkeyMask   = 0x7fff
)

// Read inspects sysfs (normally /sys/class/infiniband) and reports the node's
// tenant p_key layout, and the number of active InfiniBand ports carrying a
// tenant entry (0 when the state is Ambiguous). It never fails: a missing or
// empty tree is None, and an unreadable or malformed p_key table on an active
// InfiniBand port is Ambiguous. The count is for logging only and is not part
// of the State.
//
// Only ports whose link_layer is InfiniBand and whose state contains ACTIVE
// count. A tenant entry is a full-member p_key whose masked value is neither
// 0x0000 nor 0x7fff, the same rule as nccl-fabric-env.sh.
func Read(sysfs string) (State, int) {
	ports, _ := filepath.Glob(filepath.Join(sysfs, "*", "ports", "*"))

	var (
		found       bool
		first       State
		tenantPorts int
	)

	for _, dir := range ports {
		status, ok := portStatus(dir)
		if !ok {
			return State{Kind: Ambiguous}, 0
		}

		if status != activePort {
			continue
		}

		entries, ok := tenantEntries(dir)
		if !ok {
			return State{Kind: Ambiguous}, 0
		}

		if len(entries) > 1 {
			return State{Kind: Ambiguous}, 0
		}

		if len(entries) == 0 {
			continue
		}

		tenantPorts++

		if !found {
			found, first = true, entries[0]
		} else if entries[0] != first {
			return State{Kind: Ambiguous}, 0
		}
	}

	switch {
	case !found:
		return State{Kind: None}, 0
	case first.Index == 0:
		return State{Kind: Slot0, PKey: first.PKey}, tenantPorts
	default:
		return first, tenantPorts
	}
}

type portKind int

const (
	otherPort portKind = iota // not InfiniBand, not ACTIVE, or vanished
	activePort
)

// portStatus classifies a port. ok is false when link_layer or state cannot be
// read for any reason other than the port having vanished: a port we cannot
// classify might disagree with the others, so the node is in doubt.
func portStatus(dir string) (kind portKind, ok bool) {
	ll, err := readTrim(filepath.Join(dir, "link_layer"))
	if err != nil {
		return otherPort, vanished(dir, err)
	}

	state, err := readTrim(filepath.Join(dir, "state"))
	if err != nil {
		return otherPort, vanished(dir, err)
	}

	if ll == "InfiniBand" && strings.Contains(state, "ACTIVE") {
		return activePort, true
	}

	return otherPort, true
}

// vanished reports whether err just means the port directory went away.
func vanished(dir string, err error) bool {
	if !errors.Is(err, fs.ErrNotExist) {
		return false
	}

	_, statErr := os.Stat(dir)

	return errors.Is(statErr, fs.ErrNotExist)
}

func readTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}

// tenantEntries returns the port's tenant entries as Indexed states. ok is
// false when the table cannot be read or an entry is malformed.
func tenantEntries(dir string) (entries []State, ok bool) {
	files, err := os.ReadDir(filepath.Join(dir, "pkeys"))
	if err != nil {
		return nil, false
	}

	for _, f := range files {
		index, err := strconv.Atoi(f.Name())
		if err != nil || index < 0 {
			return nil, false
		}

		raw, err := readTrim(filepath.Join(dir, "pkeys", f.Name()))
		if err != nil {
			return nil, false
		}

		// sysfs always writes 0x%04x; accept nothing else, so an octal or
		// underscored form cannot be misread.
		hex, found := strings.CutPrefix(raw, "0x")
		if !found {
			return nil, false
		}

		v, err := strconv.ParseUint(hex, 16, 16)
		if err != nil {
			return nil, false
		}

		if v&fullMember == 0 {
			continue
		}

		p := uint16(v & pkeyMask)
		if p == 0 || p == pkeyMask {
			continue
		}

		entries = append(entries, State{Kind: Indexed, PKey: p, Index: index})
	}

	return entries, true
}
