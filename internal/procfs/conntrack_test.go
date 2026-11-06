package procfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConntrackLineTCP(t *testing.T) {
	line := "ipv4     2 tcp      6 95 TIME_WAIT src=10.20.8.29 dst=10.70.8.1 sport=45397 dport=161 src=10.70.8.1 dst=10.20.8.29 sport=161 dport=45397 [ASSURED] mark=0 zone=0 use=2"
	e, ok := ParseConntrackLine(line)
	if !ok {
		t.Fatal("не разобрана строка")
	}
	if e.Status != "SAC" || e.Src != "10.20.8.29" || e.SrcPort != 45397 ||
		e.Dst != "10.70.8.1" || e.DstPort != 161 {
		t.Fatalf("%+v", e)
	}
	if e.ReplySrc != "10.70.8.1" || e.ReplyDst != "10.20.8.29" {
		t.Fatalf("reply: %+v", e)
	}
	if e.Proto != "tcp" || e.ProtoNum != 6 || e.TCPState != "TIME_WAIT" || e.Timeout != 95 {
		t.Fatalf("proto/state: %+v", e)
	}
	if FormatTimeout(e.Timeout) != "00:01:35" {
		t.Fatalf("timeout: %s", FormatTimeout(e.Timeout))
	}
}

func TestParseConntrackLineUnreplied(t *testing.T) {
	line := "ipv4     2 udp      17 13 src=10.11.10.231 dst=255.255.255.255 sport=5678 dport=5678 [UNREPLIED] src=255.255.255.255 dst=10.11.10.231 sport=5678 dport=5678 mark=0 use=2"
	e, ok := ParseConntrackLine(line)
	if !ok {
		t.Fatal("не разобрана строка")
	}
	if e.Status != "C" {
		t.Fatalf("status=%q", e.Status)
	}
	if e.Proto != "udp" || e.SrcPort != 5678 {
		t.Fatalf("%+v", e)
	}
}

func TestReadConntrackForIPAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nf_conntrack")
	data := "" +
		"ipv4 2 tcp 6 95 TIME_WAIT src=10.0.0.2 dst=10.0.0.1 sport=1234 dport=80 src=10.0.0.1 dst=10.0.0.2 sport=80 dport=1234 [ASSURED] mark=0\n" +
		"ipv4 2 udp 17 10 src=10.0.0.9 dst=10.0.0.1 sport=5000 dport=53 src=10.0.0.1 dst=10.0.0.9 sport=53 dport=5000 mark=0\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadConntrackForIPAt(path, "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Src != "10.0.0.2" {
		t.Fatalf("%+v", rows)
	}
}
