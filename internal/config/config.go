// Package config хранит настройки приложения.
//
// Все учётные данные (логин/пароль БД) и параметры стенда читаются из
// JSON-файла настроек. В коде нет паролей и логинов по умолчанию.
// Путь по умолчанию: /etc/accel-menu-go/config.json, переопределяется
// переменной ACCEL_MENU_CONFIG.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Значения по умолчанию для не-секретных параметров. Учётные данные БД
// намеренно отсутствуют — они обязательны в файле настроек.
const (
	DefaultPath = "/etc/accel-menu-go/config.json"

	DefaultDBHost = "127.0.0.1:3306"
	DefaultDBName = "radius"

	DefaultIface     = "enp1s0f1np1"
	DefaultLogPath   = "/var/log/accel-ppp/accel-ppp.log"
	DefaultEmerg     = "/var/log/accel-ppp/emerg.log"
	DefaultAccelConf = "/etc/accel-ppp.conf"

	DefaultNFTFamily = "inet"
	DefaultNFTTable  = "filter"

	DefaultNetMain  = "10.92.0.0/24"
	DefaultNetGuest = "10.0.0.0/24"

	DefaultAccelUnit = "accel-ppp"
	DefaultRadiusCTR = "bras-freeradius"
	DefaultDBCTR     = "bras-mariadb"

	DefaultRefresh    = 2 * time.Second
	DefaultSlow       = 10 * time.Second
	DefaultCmdTimeout = 5 * time.Second
	DefaultTailLines  = 500
	DefaultTailBytes  = 512 * 1024
)

// ACLSet — описание блокировочного nft set-а.
type ACLSet struct {
	Name string
	Desc string
}

// DefaultACLSets — штатные set-ы биллинга (как в bash-скрипте).
var DefaultACLSets = []ACLSet{
	{"ipset_1", "без денег (заглушка :81)"},
	{"ipset_freezee", "заморожен биллингом (:82)"},
	{"ipset_disabled", "отключён абонентом (:83)"},
	{"ipset_deleted", "удалён биллингом (:84)"},
	{"ipset_unknown", "неизвестный абонент (:85)"},
	{"allowed_ipset", "разрешённые направления"},
}

// Config — полная конфигурация приложения.
type Config struct {
	DBHost string
	DBUser string
	DBPass string
	DBName string

	Iface     string
	LogPath   string
	EmergPath string
	AccelConf string
	// UplinkIface пустой — определить по маршруту по умолчанию.
	UplinkIface string

	NFTFamily string
	NFTTable  string

	NetMain  string
	NetGuest string

	AccelUnit   string
	RadiusCTR   string
	DBCTR       string
	ACLSets     []ACLSet
	Refresh     time.Duration
	SlowRefresh time.Duration
	CmdTimeout  time.Duration
	TailLines   int
	TailBytes   int64
	AccelCmd    string
	Version     string

	// ConfigPath и ConfigMode — для диагностики (права на файл с паролем).
	ConfigPath string
	ConfigMode os.FileMode
}

// fileConfig — JSON-схема файла настроек. Все поля необязательны, кроме
// database.user и database.password.
type fileConfig struct {
	Database *struct {
		Host     string `json:"host"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
	} `json:"database"`
	Iface       string `json:"iface"`
	LogPath     string `json:"log_path"`
	EmergPath   string `json:"emerg_path"`
	AccelConf   string `json:"accel_conf"`
	UplinkIface string `json:"uplink_iface"`
	NFT         *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
	} `json:"nft"`
	Networks *struct {
		Main  string `json:"main"`
		Guest string `json:"guest"`
	} `json:"networks"`
	AccelUnit       string `json:"accel_unit"`
	RadiusContainer string `json:"radius_container"`
	DBContainer     string `json:"db_container"`
	Refresh         string `json:"refresh"`
	SlowRefresh     string `json:"slow_refresh"`
	CmdTimeout      string `json:"cmd_timeout"`
	TailLines       int    `json:"tail_lines"`
	TailBytes       int64  `json:"tail_bytes"`
	AccelCmd        string `json:"accel_cmd"`
}

// Defaults возвращает не-секретные значения по умолчанию.
// Учётные данные БД пустые: без файла настроек приложение работать не должно.
func Defaults() Config {
	return Config{
		DBHost:      DefaultDBHost,
		DBName:      DefaultDBName,
		Iface:       DefaultIface,
		LogPath:     DefaultLogPath,
		EmergPath:   DefaultEmerg,
		AccelConf:   DefaultAccelConf,
		NFTFamily:   DefaultNFTFamily,
		NFTTable:    DefaultNFTTable,
		NetMain:     DefaultNetMain,
		NetGuest:    DefaultNetGuest,
		AccelUnit:   DefaultAccelUnit,
		RadiusCTR:   DefaultRadiusCTR,
		DBCTR:       DefaultDBCTR,
		ACLSets:     append([]ACLSet(nil), DefaultACLSets...),
		Refresh:     DefaultRefresh,
		SlowRefresh: DefaultSlow,
		CmdTimeout:  DefaultCmdTimeout,
		TailLines:   DefaultTailLines,
		TailBytes:   DefaultTailBytes,
		AccelCmd:    "accel-cmd",
		Version:     "dev",
	}
}

// Path возвращает путь к файлу настроек.
func Path() string {
	if p := strings.TrimSpace(os.Getenv("ACCEL_MENU_CONFIG")); p != "" {
		return p
	}
	return DefaultPath
}

// Load читает файл настроек и применяет значения поверх Defaults().
// Логин и пароль БД обязательны — иначе возвращается ошибка.
func Load() (Config, error) {
	path := Path()
	cfg := Defaults()
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("файл настроек %s: %w", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return cfg, fmt.Errorf("файл настроек %s: %w", path, err)
	}
	cfg.ConfigMode = fi.Mode()

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return cfg, fmt.Errorf("файл настроек %s: %w", path, err)
	}

	if fc.Database != nil {
		if v := strings.TrimSpace(fc.Database.Host); v != "" {
			cfg.DBHost = v
		}
		cfg.DBUser = strings.TrimSpace(fc.Database.User)
		cfg.DBPass = fc.Database.Password
		if v := strings.TrimSpace(fc.Database.Name); v != "" {
			cfg.DBName = v
		}
	}
	if v := strings.TrimSpace(fc.Iface); v != "" {
		cfg.Iface = v
	}
	if v := strings.TrimSpace(fc.LogPath); v != "" {
		cfg.LogPath = v
	}
	if v := strings.TrimSpace(fc.EmergPath); v != "" {
		cfg.EmergPath = v
	}
	if v := strings.TrimSpace(fc.AccelConf); v != "" {
		cfg.AccelConf = v
	}
	if v := strings.TrimSpace(fc.UplinkIface); v != "" {
		cfg.UplinkIface = v
	}
	if fc.NFT != nil {
		if v := strings.TrimSpace(fc.NFT.Family); v != "" {
			cfg.NFTFamily = v
		}
		if v := strings.TrimSpace(fc.NFT.Table); v != "" {
			cfg.NFTTable = v
		}
	}
	if fc.Networks != nil {
		if v := strings.TrimSpace(fc.Networks.Main); v != "" {
			cfg.NetMain = v
		}
		if v := strings.TrimSpace(fc.Networks.Guest); v != "" {
			cfg.NetGuest = v
		}
	}
	if v := strings.TrimSpace(fc.AccelUnit); v != "" {
		cfg.AccelUnit = v
	}
	if v := strings.TrimSpace(fc.RadiusContainer); v != "" {
		cfg.RadiusCTR = v
	}
	if v := strings.TrimSpace(fc.DBContainer); v != "" {
		cfg.DBCTR = v
	}
	if v := strings.TrimSpace(fc.AccelCmd); v != "" {
		cfg.AccelCmd = v
	}

	var derr error
	if cfg.Refresh, derr = parseDuration(fc.Refresh, cfg.Refresh); derr != nil {
		return cfg, fmt.Errorf("refresh: %w", derr)
	}
	if cfg.SlowRefresh, derr = parseDuration(fc.SlowRefresh, cfg.SlowRefresh); derr != nil {
		return cfg, fmt.Errorf("slow_refresh: %w", derr)
	}
	if cfg.CmdTimeout, derr = parseDuration(fc.CmdTimeout, cfg.CmdTimeout); derr != nil {
		return cfg, fmt.Errorf("cmd_timeout: %w", derr)
	}
	if fc.TailLines > 0 {
		cfg.TailLines = fc.TailLines
	}
	if fc.TailBytes > 0 {
		cfg.TailBytes = fc.TailBytes
	}
	if cfg.TailLines < 50 {
		cfg.TailLines = 50
	}
	if cfg.TailBytes < 64*1024 {
		cfg.TailBytes = 64 * 1024
	}

	if cfg.DBUser == "" || cfg.DBPass == "" {
		return cfg, fmt.Errorf("в %s не заданы database.user/database.password", path)
	}
	if cfg.DBName == "" {
		cfg.DBName = DefaultDBName
	}
	if cfg.DBHost == "" {
		cfg.DBHost = DefaultDBHost
	}
	return cfg, nil
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("должно быть > 0")
	}
	return d, nil
}
