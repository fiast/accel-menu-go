package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultsHasNoCredentials(t *testing.T) {
	c := Defaults()
	if c.DBUser != "" || c.DBPass != "" {
		t.Fatalf("в коде не должно быть логина/пароля: %q/%q", c.DBUser, c.DBPass)
	}
	if c.Iface == "" || c.Refresh == 0 {
		t.Fatalf("не заданы не-секретные умолчания: %+v", c)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
	  "database": {"host": "127.0.0.1:3306", "user": "dbuser", "password": "dbpass", "name": "radius"},
	  "iface": "eth9",
	  "accel_conf": "/tmp/accel-ppp.conf",
	  "uplink_iface": "wan0",
	  "refresh": "3s",
	  "tail_lines": 100
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCEL_MENU_CONFIG", path)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DBUser != "dbuser" || c.DBPass != "dbpass" {
		t.Fatalf("учётные данные не прочитаны: %+v", c)
	}
	if c.Iface != "eth9" || c.Refresh != 3*time.Second || c.TailLines != 100 {
		t.Fatalf("настройки не применены: %+v", c)
	}
	if c.AccelConf != "/tmp/accel-ppp.conf" || c.UplinkIface != "wan0" {
		t.Fatalf("новые поля не прочитаны: %+v", c)
	}
	if c.ConfigPath != path || c.ConfigMode.Perm() != 0o600 {
		t.Fatalf("путь/права: %q %v", c.ConfigPath, c.ConfigMode)
	}
}

func TestLoadRequiresCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"iface":"eth0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCEL_MENU_CONFIG", path)
	if _, err := Load(); err == nil {
		t.Fatal("ожидалась ошибка об отсутствии логина/пароля")
	}
}

func TestLoadBadDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"database":{"user":"u","password":"p"},"refresh":"not-a-duration"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCEL_MENU_CONFIG", path)
	if _, err := Load(); err == nil {
		t.Fatal("ожидалась ошибка duration")
	}
}

func TestPathEnv(t *testing.T) {
	t.Setenv("ACCEL_MENU_CONFIG", "/tmp/custom.json")
	if Path() != "/tmp/custom.json" {
		t.Fatalf("Path=%q", Path())
	}
}
