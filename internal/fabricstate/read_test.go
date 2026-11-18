package fabricstate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// port builds <root>/<dev>/ports/<port> with the given link layer, state and
// p_key table (pkeys[i] is the content of pkeys/i).
func port(t *testing.T, root, dev, port, linkLayer, state string, pkeys ...string) {
	t.Helper()

	dir := filepath.Join(root, dev, "ports", port)
	if err := os.MkdirAll(filepath.Join(dir, "pkeys"), 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(dir, "link_layer"), linkLayer)
	write(filepath.Join(dir, "state"), state)

	for i, p := range pkeys {
		write(filepath.Join(dir, "pkeys", strconv.Itoa(i)), p)
	}
}

const active = "4: ACTIVE"

func TestHFMLayoutIsIndexed(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600", "0x0000")
	port(t, root, "mlx5_1", "1", "InfiniBand", active, "0x7fff", "0x8600", "0x0000")

	got, ports := Read(root)
	if want := (State{Kind: Indexed, PKey: 0x0600, Index: 1}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if ports != 2 {
		t.Fatalf("ports = %d, want 2", ports)
	}
}

func TestHandMadeLayoutIsSlot0(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x8030", "0x7fff")

	if got, _ := Read(root); got != (State{Kind: Slot0, PKey: 0x0030}) {
		t.Fatalf("got %+v, want Slot0 with PKey 0x0030", got)
	}
}

func TestManagementPortsIgnored(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_6", "1", "InfiniBand", active, "0xffff")
	port(t, root, "mlx5_7", "1", "InfiniBand", active, "0x7fff")

	got, ports := Read(root)
	if got.Kind != Indexed || got.Index != 1 || got.PKey != 0x0600 {
		t.Fatalf("got %+v, want Indexed 0x0600 at 1", got)
	}

	if ports != 1 {
		t.Fatalf("ports = %d, want 1 (management ports carry no tenant entry)", ports)
	}
}

func TestInactivePortsIgnored(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", "1: DOWN", "0x7fff", "0x8600")

	if got, _ := Read(root); got.Kind != None {
		t.Fatalf("got %+v, want None", got)
	}
}

func TestNoInfiniBandIsNone(t *testing.T) {
	if got, _ := Read(t.TempDir()); got.Kind != None {
		t.Fatalf("empty: got %+v, want None", got)
	}

	if got, _ := Read(filepath.Join(t.TempDir(), "missing")); got.Kind != None {
		t.Fatalf("missing: got %+v, want None", got)
	}

	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "Ethernet", active, "0x7fff", "0x8600")

	if got, _ := Read(root); got.Kind != None {
		t.Fatalf("ethernet: got %+v, want None", got)
	}
}

func TestDisagreeingPortsAreAmbiguous(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", active, "0x7fff", "0x0000", "0x8600")

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("index: got %+v, want Ambiguous", got)
	}

	root = t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", active, "0x7fff", "0x8601")

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("pkey: got %+v, want Ambiguous", got)
	}
}

func TestTwoTenantsOnOnePortAreAmbiguous(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600", "0x8601")

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("got %+v, want Ambiguous", got)
	}
}

func TestGarbagePKeyFileIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600", "zz")

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("got %+v, want Ambiguous", got)
	}
}

func TestLimitedMemberIsNotATenant(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x0600")

	if got, _ := Read(root); got.Kind != None {
		t.Fatalf("got %+v, want None", got)
	}
}

func TestUnreadableStateIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", active, "0x8030", "0x7fff")

	state := filepath.Join(root, "mlx5_1", "ports", "1", "state")
	if err := os.Chmod(state, 0); err != nil {
		t.Fatal(err)
	}

	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("unreadable: got %+v, want Ambiguous", got)
	}

	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("missing file in a present port: got %+v, want Ambiguous", got)
	}
}

func TestVanishedPortIsSkipped(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", active, "0x8030", "0x7fff")

	if err := os.RemoveAll(filepath.Join(root, "mlx5_1")); err != nil {
		t.Fatal(err)
	}

	if got, _ := Read(root); got.Kind != Indexed {
		t.Fatalf("got %+v, want Indexed", got)
	}

	// vanished() directly: a file error under a directory that is gone.
	if !vanished(filepath.Join(root, "mlx5_1", "ports", "1"), fs.ErrNotExist) {
		t.Fatal("a removed port directory should count as vanished")
	}
}

func TestLenientPKeyFormsAreAmbiguous(t *testing.T) {
	for _, v := range []string{"0600", "0x_8600", "8600", "0X8600", ""} {
		root := t.TempDir()
		port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", v)

		if got, _ := Read(root); got.Kind != Ambiguous {
			t.Fatalf("%q: got %+v, want Ambiguous", v, got)
		}
	}
}

func TestActivePortWithoutPKeysDirIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", active)

	if err := os.Remove(filepath.Join(root, "mlx5_1", "ports", "1", "pkeys")); err != nil {
		t.Fatal(err)
	}

	if got, _ := Read(root); got.Kind != Ambiguous {
		t.Fatalf("got %+v, want Ambiguous", got)
	}
}

func TestDisagreeingInactivePortIgnored(t *testing.T) {
	root := t.TempDir()
	port(t, root, "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")
	port(t, root, "mlx5_1", "1", "InfiniBand", "1: DOWN", "0x8030", "0x7fff")

	if got, _ := Read(root); got.Kind != Indexed {
		t.Fatalf("got %+v, want Indexed", got)
	}
}

// Real /sys/class/infiniband entries are relative symlinks into /sys/devices,
// so the plugin must see the whole sysfs: with class/ alone they dangle and the
// node reads as None.
func TestClassSymlinksResolveThroughDevices(t *testing.T) {
	root := t.TempDir()
	port(t, filepath.Join(root, "devices", "pci0", "infiniband"), "mlx5_0", "1", "InfiniBand", active, "0x7fff", "0x8600")

	class := filepath.Join(root, "class", "infiniband")
	if err := os.MkdirAll(class, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("../../devices/pci0/infiniband/mlx5_0", filepath.Join(class, "mlx5_0")); err != nil {
		t.Fatal(err)
	}

	if got, _ := Read(class); got != (State{Kind: Indexed, PKey: 0x0600, Index: 1}) {
		t.Fatalf("whole tree: got %+v, want Indexed 0x0600 at 1", got)
	}

	if err := os.RemoveAll(filepath.Join(root, "devices")); err != nil {
		t.Fatal(err)
	}

	if got, _ := Read(class); got.Kind != None {
		t.Fatalf("class/ only: got %+v, want None", got)
	}
}
