package procfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const loadavgSample = "0.04 0.02 0.00 1/323 2812900\n"

func TestParseLoadAvg(t *testing.T) {
	l, err := ParseLoadAvg(loadavgSample)
	if err != nil {
		t.Fatalf("ParseLoadAvg: %v", err)
	}
	if l.One != 0.04 || l.Five != 0.02 || l.Fifteen != 0 {
		t.Fatalf("неверные значения: %+v", l)
	}
}

const meminfoSample = `MemTotal:       15518164 kB
MemFree:         6285432 kB
MemAvailable:   14173356 kB
Buffers:         1111188 kB
Cached:          6668164 kB
`

func TestParseMemInfo(t *testing.T) {
	m, err := ParseMemInfo(meminfoSample)
	if err != nil {
		t.Fatalf("ParseMemInfo: %v", err)
	}
	if m.TotalKB != 15518164 || m.AvailableKB != 14173356 {
		t.Fatalf("неверные значения: %+v", m)
	}
	if m.UsedKB != 1344808 {
		t.Fatalf("used=%d", m.UsedKB)
	}
}

func TestParseMemInfoFallback(t *testing.T) {
	m, err := ParseMemInfo("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 150 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.AvailableKB != 300 || m.UsedKB != 700 {
		t.Fatalf("%+v", m)
	}
}

func TestParseMemInfoNoTotal(t *testing.T) {
	if _, err := ParseMemInfo("MemFree: 1 kB\n"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestReadNetStatsAt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "eth0", "statistics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"rx_bytes": "123", "tx_bytes": "456", "rx_packets": "7", "tx_packets": "8",
	}
	for name, val := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := ReadNetStatsAt(root, "eth0")
	if err != nil {
		t.Fatal(err)
	}
	if st.RxBytes != 123 || st.TxBytes != 456 || st.RxPackets != 7 || st.TxPackets != 8 {
		t.Fatalf("%+v", st)
	}
}

func TestReadModulesAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules")
	data := "ipoe 49152 0 - Live 0x0 (OE)\nvlan_mon 16384 0 - Live 0x0 (OE)\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err := ReadModulesAt(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"ipoe", "vlan_mon"} {
		if _, ok := mods[m]; !ok {
			t.Fatalf("нет модуля %s", m)
		}
	}
}

func TestReadConntrackCountAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nf_conntrack")
	data := strings.Join([]string{
		"ipv4 2 tcp 6 95 TIME_WAIT src=192.0.2.2 dst=8.8.8.8 sport=1 dport=2",
		"ipv4 2 udp 17 10 src=198.51.100.5 dst=1.1.1.1 sport=3 dport=4",
		"ipv4 2 tcp 6 95 TIME_WAIT src=127.0.0.1 dst=127.0.0.1 sport=5 dport=6",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ReadConntrackCountAt(path, []string{"src=192.0.2.", "src=198.51.100."})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count=%d", n)
	}
}

func TestSplitCIDRPrefix(t *testing.T) {
	cases := map[string]string{
		"10.92.0.0/24": "10.92.0.",
		"10.0.0.0/24":  "10.0.0.",
		"192.168.1.0":  "192.168.1.",
	}
	for in, want := range cases {
		if got := SplitCIDRPrefix(in); got != want {
			t.Fatalf("%s -> %s, want %s", in, got, want)
		}
	}
}
