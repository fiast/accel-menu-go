package service

import (
	"context"
	"strings"
	"testing"
)

func TestCreateSubscriberTypeValidation(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()
	err := svc.CreateSubscriber(ctx, NewSubscriberInput{
		Username: "not-a-mac",
		IsIPoE:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "MAC") {
		t.Fatalf("IPoE без MAC должен отклоняться: %v", err)
	}
	err = svc.CreateSubscriber(ctx, NewSubscriberInput{
		Username: "user1",
		IsIPoE:   false,
		Password: "",
	})
	if err == nil || !strings.Contains(err.Error(), "пароль") {
		t.Fatalf("PPPoE без пароля должен отклоняться: %v", err)
	}
}

func TestStatusLabels(t *testing.T) {
	cases := []struct {
		st    Status
		label string
		str   string
	}{
		{OK, "[ OK ]", "OK"},
		{Warn, "[WARN]", "WARN"},
		{Fail, "[FAIL]", "FAIL"},
	}
	for _, c := range cases {
		if c.st.Label() != c.label || c.st.String() != c.str {
			t.Fatalf("%v: %q %q", c.st, c.st.Label(), c.st.String())
		}
	}
}

func TestHasFail(t *testing.T) {
	if HasFail([]Check{{Status: OK}, {Status: Warn}}) {
		t.Fatal("ложный FAIL")
	}
	if !HasFail([]Check{{Status: OK}, {Status: Fail}}) {
		t.Fatal("FAIL не найден")
	}
	if ok, warn, fail := CountsByStatus([]Check{{Status: OK}, {Status: Warn}, {Status: Fail}}); ok != 1 || warn != 1 || fail != 1 {
		t.Fatalf("%d %d %d", ok, warn, fail)
	}
}

func TestValidateLogin(t *testing.T) {
	for _, good := range []string{"demo_user_01", "02:00:00:00:00:01", "user.name@x", "a_b-c"} {
		if err := ValidateLogin(good); err != nil {
			t.Fatalf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{"", "a b", "user;reboot", "юзер"} {
		if err := ValidateLogin(bad); err == nil {
			t.Fatalf("должно быть невалидно: %q", bad)
		}
	}
}

func TestValidateIPv4(t *testing.T) {
	for _, good := range []string{"", "192.0.2.2", "255.255.255.255"} {
		if err := ValidateIPv4(good); err != nil {
			t.Fatalf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"10.92.0", "192.0.2.256", "abc", "192.0.2.2/24"} {
		if err := ValidateIPv4(bad); err == nil {
			t.Fatalf("должно быть невалидно: %q", bad)
		}
	}
}

func TestValidateSpeed(t *testing.T) {
	for _, good := range []string{"1", "50000", "100000000"} {
		if err := ValidateSpeed(good); err != nil {
			t.Fatalf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{"", "0", "-1", "abc", "100000001"} {
		if err := ValidateSpeed(bad); err == nil {
			t.Fatalf("должно быть невалидно: %q", bad)
		}
	}
}
