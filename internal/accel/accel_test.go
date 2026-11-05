package accel

import (
	"os"
	"path/filepath"
	"testing"
)

const statSample = "uptime: 0.12:54:50\r\n" +
	"cpu: 0%\r\n" +
	"mem(rss/virt): 9736/321048 kB\r\n" +
	"core:\r\n" +
	"  mempool_allocated: 263610\r\n" +
	"  thread_count: 4\r\n" +
	"sessions:\r\n" +
	"  starting: 0\r\n" +
	"  active: 1\r\n" +
	"  finishing: 0\r\n" +
	"pppoe:\r\n" +
	"  starting: 0\r\n" +
	"  active: 0\r\n" +
	"  delayed PADO: 0\r\n" +
	"l2tp:\r\n" +
	"  tunnels:\r\n" +
	"    active: 0\r\n" +
	"radius(1, 127.0.0.1):\r\n" +
	"  state: active\r\n" +
	"  fail count: 0\r\n" +
	"  auth sent: 13296\r\n" +
	"ipoe:\r\n" +
	"  starting: 0\r\n" +
	"  active: 1\r\n"

func TestParseStat(t *testing.T) {
	st, err := ParseStat(statSample)
	if err != nil {
		t.Fatal(err)
	}
	if st.Uptime != "0.12:54:50" || st.CPU != "0%" {
		t.Fatalf("%+v", st)
	}
	if st.RSSKB != 9736 || st.VirtKB != 321048 {
		t.Fatalf("mem: %+v", st)
	}
	if st.SessionsActive != 1 || st.SessionsStarting != 0 {
		t.Fatalf("sessions: %+v", st)
	}
	if st.PPPoEActive != 0 || st.IPoEActive != 1 {
		t.Fatalf("pppoe/ipoe: %+v", st)
	}
	if st.RadiusState != "active" || st.RadiusFail != 0 {
		t.Fatalf("radius: %+v", st)
	}
	if len(st.Modules) != 5 {
		t.Fatalf("modules: %+v", st.Modules)
	}
	byName := map[string]Module{}
	for _, m := range st.Modules {
		byName[m.Name] = m
	}
	if m := byName["pppoe"]; !m.Present || m.Active != 0 {
		t.Fatalf("pppoe: %+v", m)
	}
	if m := byName["ipoe"]; !m.Present || m.Active != 1 {
		t.Fatalf("ipoe: %+v", m)
	}
	if m := byName["l2tp"]; !m.Present {
		t.Fatalf("l2tp: %+v", m)
	}
	if m := byName["pptp"]; m.Present {
		t.Fatalf("pptp: %+v", m)
	}
}

func TestParseStatGarbage(t *testing.T) {
	if _, err := ParseStat("hello world\n"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

const sessionsSample = "" +
	"     ifname      |     username      |    calling-sid    |    ip    | rate-limit | type | comp | state  |  uptime  \n" +
	"-----------------+-------------------+-------------------+----------+------------+------+------+--------+----------\n" +
	" eth0.100 | 02:00:00:00:00:01 | 02:00:00:00:00:01 | 192.0.2.2 | 1000/1000  | ipoe |      | active | 12:51:27 \n"

func TestParseSessions(t *testing.T) {
	got := ParseSessions(sessionsSample)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	s := got[0]
	if s.IfName != "eth0.100" || s.Username != "02:00:00:00:00:01" ||
		s.IP != "192.0.2.2" || s.Type != "ipoe" || s.State != "active" {
		t.Fatalf("%+v", s)
	}
}

func TestParseSessionsEmpty(t *testing.T) {
	got := ParseSessions("ifname | username\n----+----\n")
	if len(got) != 0 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestParseStatLinesAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(path, []byte(statSample), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseStat(string(data)); err != nil {
		t.Fatal(err)
	}
}
