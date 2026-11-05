package docker

import "testing"

func TestParseStateHealthy(t *testing.T) {
	st, err := ParseState("bras-mariadb", `{"Status":"running","Health":{"Status":"healthy"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running() || !st.Healthy() || st.Display() != "running / healthy" {
		t.Fatalf("%+v", st)
	}
}

func TestParseStateNoHealth(t *testing.T) {
	st, err := ParseState("bras-freeradius", `{"Status":"running"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running() || st.Healthy() || st.Display() != "running" {
		t.Fatalf("%+v", st)
	}
}

func TestParseStateBadJSON(t *testing.T) {
	if _, err := ParseState("x", "not json"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
