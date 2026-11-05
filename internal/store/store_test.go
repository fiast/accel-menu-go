package store

import "testing"

func TestIsMAC(t *testing.T) {
	good := []string{
		"02:00:00:00:00:01",
		"78:B2:13:38:31:8E",
		"00:00:00:00:00:00",
	}
	for _, s := range good {
		if !IsMAC(s) {
			t.Fatalf("должно быть MAC: %s", s)
		}
	}
	bad := []string{
		"", "demo_user_01", "02:00:00:00:00", "02-00-00-00-00-01",
		"02:00:00:00:00:0g", "02:00:00:00:00:01:00",
	}
	for _, s := range bad {
		if IsMAC(s) {
			t.Fatalf("не должно быть MAC: %s", s)
		}
	}
}
