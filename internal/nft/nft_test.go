package nft

import (
	"strings"
	"testing"
)

const setsJSON = `{"nftables": [
 {"metainfo": {"version": "1.1.3", "json_schema_version": 1}},
 {"table": {"family": "inet", "name": "filter"}},
 {"chain": {"family": "inet", "table": "filter", "name": "postrouting"}},
 {"set": {"family": "inet", "name": "ipset_1", "table": "filter", "type": "ipv4_addr", "flags": ["timeout"], "elem": ["100.70.0.233", "100.70.0.236"]}},
 {"set": {"family": "inet", "name": "ipset_freezee", "table": "filter", "type": "ipv4_addr", "flags": ["timeout"]}},
 {"set": {"family": "inet", "name": "allowed_ipset", "table": "filter", "type": "ipv4_addr"}}
]}`

func TestParseSetsJSON(t *testing.T) {
	sets, err := ParseSetsJSON(setsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 3 {
		t.Fatalf("len=%d", len(sets))
	}
	byName := map[string]Set{}
	for _, s := range sets {
		byName[s.Name] = s
	}
	if byName["ipset_1"].Count != 2 {
		t.Fatalf("ipset_1: %+v", byName["ipset_1"])
	}
	if len(byName["ipset_1"].Elements) != 2 || byName["ipset_1"].Elements[0] != "100.70.0.233" {
		t.Fatalf("ipset_1 elements: %+v", byName["ipset_1"].Elements)
	}
	if byName["ipset_freezee"].Count != 0 {
		t.Fatalf("freezee: %+v", byName["ipset_freezee"])
	}
}

const elemText = `table inet filter {
	set ipset_1 {
		type ipv4_addr
		flags timeout
		elements = { 100.70.0.233 timeout 5m expires 4m58s992ms,
			     100.70.0.236 timeout 10m,
			     100.70.1.39 }
	}
}`

func TestParseElements(t *testing.T) {
	els := ParseElements(elemText)
	if len(els) != 3 {
		t.Fatalf("len=%d: %+v", len(els), els)
	}
	if els[0].IP != "100.70.0.233" || els[0].TTL != "4m58s" {
		t.Fatalf("%+v", els[0])
	}
	if els[1].TTL != "10m" {
		t.Fatalf("%+v", els[1])
	}
	if els[2].TTL != "" {
		t.Fatalf("%+v", els[2])
	}
}

func TestParseElementsEmpty(t *testing.T) {
	raw := "table inet filter {\n\tset ipset_freezee {\n\t\ttype ipv4_addr\n\t\tflags timeout\n\t}\n}\n"
	if els := ParseElements(raw); len(els) != 0 {
		t.Fatalf("%+v", els)
	}
}

func TestParseElementsSingleLine(t *testing.T) {
	raw := "elements = { 192.0.2.2, 10.0.0.3 }"
	els := ParseElements(raw)
	if len(els) != 2 || els[1].IP != "10.0.0.3" {
		t.Fatalf("%+v", els)
	}
}

func TestParseSetsJSONObjects(t *testing.T) {
	raw := `{"nftables":[{"set":{"name":"s1","elem":[
	  {"elem":{"val":"10.1.2.3","timeout":300}},
	  {"val":"10.1.2.4"}
	]}}]}`
	sets, err := ParseSetsJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || len(sets[0].Elements) != 2 ||
		sets[0].Elements[0] != "10.1.2.3" || sets[0].Elements[1] != "10.1.2.4" {
		t.Fatalf("%+v", sets)
	}
}

func TestValidSetName(t *testing.T) {
	for _, good := range []string{"ipset_1", "allowed_ipset", "ipset_abon"} {
		if !ValidSetName(good) {
			t.Fatalf("должно быть валидно: %s", good)
		}
	}
	for _, bad := range []string{"", "a b", "x;reboot", "set{", strings.Repeat("a", 65)} {
		if ValidSetName(bad) {
			t.Fatalf("должно быть невалидно: %q", bad)
		}
	}
}

func TestMasqueradeInterfaces(t *testing.T) {
	text := `table inet filter {
	chain postrouting {
		oifname "vlan4094" masquerade
		ip saddr 10.92.0.0/24 oifname "enp7s0" masquerade
		ip saddr 10.0.0.0/24 oifname "enp7s0" masquerade
	}
}`
	ifaces := MasqueradeInterfaces(text)
	if len(ifaces) != 2 || ifaces[0] != "vlan4094" || ifaces[1] != "enp7s0" {
		t.Fatalf("%+v", ifaces)
	}
	if !HasNATFor(text, "10.92.0.0/24") {
		t.Fatal("NAT для 10.92.0.0/24 должен быть найден")
	}
	if HasNATFor(text, "10.92.1.0/24") {
		t.Fatal("ложное срабатывание")
	}
	if m := MatchNATFor(text, "10.92.0.0/24"); len(m) != 1 {
		t.Fatalf("%+v", m)
	}
}
