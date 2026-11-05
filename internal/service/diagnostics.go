package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"accel-menu/internal/nft"
	"accel-menu/internal/procfs"
)

// Status — результат проверки.
type Status int

const (
	OK Status = iota
	Warn
	Fail
)

// String возвращает короткое имя статуса.
func (s Status) String() string {
	switch s {
	case OK:
		return "OK"
	case Warn:
		return "WARN"
	case Fail:
		return "FAIL"
	default:
		return "?"
	}
}

// Label возвращает метку в стиле bash: [ OK ] / [WARN] / [FAIL].
func (s Status) Label() string {
	switch s {
	case OK:
		return "[ OK ]"
	case Warn:
		return "[WARN]"
	case Fail:
		return "[FAIL]"
	default:
		return "[ ?? ]"
	}
}

// Check — одна диагностическая проверка.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Diagnostics проверяет критические компоненты BRAS.
func (s *Service) Diagnostics(ctx context.Context) []Check {
	var out []Check
	add := func(name string, st Status, detail string) {
		out = append(out, Check{Name: name, Status: st, Detail: detail})
	}

	if os.Geteuid() == 0 {
		add("root", OK, "приложение запущено от root")
	} else {
		add("root", Warn, "не root: nft/логи/conntrack могут быть недоступны (используется sudo -n)")
	}

	if s.cfg.ConfigPath != "" {
		if s.cfg.ConfigMode.Perm()&0o077 != 0 {
			add("config", Warn, s.cfg.ConfigPath+" доступен группе/другим (рекомендуется 0600)")
		} else {
			add("config", OK, s.cfg.ConfigPath)
		}
	}

	active := s.systemctl(ctx, "is-active")
	enabled := s.systemctl(ctx, "is-enabled")
	detail := fmt.Sprintf("is-active=%s, is-enabled=%s", active, enabled)
	if active == "active" {
		add("accel-ppp service", OK, detail)
	} else {
		add("accel-ppp service", Fail, detail)
	}
	if enabled == "enabled" {
		add("accel-ppp autostart", OK, "systemd unit включён")
	} else {
		add("accel-ppp autostart", Warn, "unit не enabled — после ребута не поднимется")
	}

	mods, merr := procfs.ReadModules()
	if merr != nil {
		add("kernel modules", Fail, "не удалось прочитать /proc/modules: "+merr.Error())
	} else {
		for _, mod := range []string{"ipoe", "vlan_mon"} {
			if _, ok := mods[mod]; ok {
				add("module "+mod, OK, "загружен")
			} else {
				add("module "+mod, Fail, "не загружен")
			}
		}
	}

	if st, err := s.dock.Inspect(ctx, s.cfg.RadiusCTR); err != nil {
		add("container "+s.cfg.RadiusCTR, Fail, err.Error())
	} else if st.Running() {
		add("container "+s.cfg.RadiusCTR, OK, st.Display())
	} else {
		add("container "+s.cfg.RadiusCTR, Fail, st.Display())
	}

	if st, err := s.dock.Inspect(ctx, s.cfg.DBCTR); err != nil {
		add("container "+s.cfg.DBCTR, Fail, err.Error())
	} else if st.Running() {
		if st.Health == "" || st.Healthy() {
			add("container "+s.cfg.DBCTR, OK, st.Display())
		} else {
			add("container "+s.cfg.DBCTR, Fail, st.Display())
		}
	} else {
		add("container "+s.cfg.DBCTR, Fail, st.Display())
	}

	if text, err := s.nft.Postrouting(ctx); err != nil {
		add("NAT "+s.cfg.NetMain, Fail, "nft: "+err.Error())
	} else if nft.HasNATFor(text, s.cfg.NetMain) {
		add("NAT "+s.cfg.NetMain, OK, "masquerade найден в postrouting")
	} else {
		add("NAT "+s.cfg.NetMain, Fail, "в postrouting нет masquerade для "+s.cfg.NetMain)
	}

	if stat, err := s.accel.ShowStat(ctx); err != nil {
		add("accel-cmd", Fail, err.Error())
	} else {
		add("accel-cmd", OK, fmt.Sprintf("uptime %s, сессий %d (PPPoE %d, IPoE %d)",
			stat.Uptime, stat.SessionsActive, stat.PPPoEActive, stat.IPoEActive))
	}

	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.store.Ping(pctx); err != nil {
		add("MariaDB", Fail, err.Error())
	} else if stats, err := s.store.Stats(pctx); err != nil {
		add("MariaDB", Fail, err.Error())
	} else {
		add("MariaDB", OK, fmt.Sprintf("абонентов %d, radcheck %d, radreply %d, radacct %d, radpostauth %d",
			stats.Users, stats.Checks, stats.Replies, stats.Acct, stats.Auths))
	}

	if f, err := os.Open(s.cfg.LogPath); err != nil {
		add("log "+s.cfg.LogPath, Fail, err.Error())
	} else {
		fi, _ := f.Stat()
		f.Close()
		if fi != nil {
			add("log "+s.cfg.LogPath, OK, fmt.Sprintf("доступен, %d байт", fi.Size()))
		} else {
			add("log "+s.cfg.LogPath, OK, "доступен")
		}
	}

	if n, err := procfs.ReadConntrackTotal(); err != nil {
		add("conntrack", Warn, err.Error())
	} else {
		add("conntrack", OK, fmt.Sprintf("записей %d", n))
	}

	if v, err := procfs.ReadIPForward(); err != nil {
		add("ip_forward", Warn, err.Error())
	} else if v {
		add("ip_forward", OK, "включён")
	} else {
		add("ip_forward", Fail, "выключен")
	}

	return out
}

// HasFail сообщает, есть ли в списке FAIL.
func HasFail(checks []Check) bool {
	for _, c := range checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

// CountsByStatus считает статусы.
func CountsByStatus(checks []Check) (ok, warn, fail int) {
	for _, c := range checks {
		switch c.Status {
		case OK:
			ok++
		case Warn:
			warn++
		case Fail:
			fail++
		}
	}
	return
}

// TrimDetail обрезает длинную строку для табличного вывода.
func TrimDetail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}
