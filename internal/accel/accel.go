// Package accel работает с CLI демона accel-ppp через accel-cmd.
package accel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"accel-menu/internal/execx"
)

// Stat — разобранный вывод "accel-cmd show stat".
type Stat struct {
	Uptime           string
	CPU              string
	RSSKB            int64
	VirtKB           int64
	SessionsActive   int
	SessionsStarting int
	PPPoEActive      int
	IPoEActive       int
	RadiusState      string
	RadiusFail       int
	Modules          []Module
}

// Module — модуль авторизации accel-ppp (PPPoE/IPoE/L2TP/...).
type Module struct {
	Name    string
	Active  int
	Present bool
}

var moduleOrder = []string{"pppoe", "ipoe", "l2tp", "pptp", "sstp"}

// Session — строка таблицы "accel-cmd show sessions".
type Session struct {
	IfName     string
	Username   string
	CallingSID string
	IP         string
	RateLimit  string
	Type       string
	Comp       string
	State      string
	Uptime     string
}

// Client — обёртка над accel-cmd.
type Client struct {
	run *execx.Runner
	bin string
}

// New создаёт клиент.
func New(run *execx.Runner, bin string) *Client {
	if bin == "" {
		bin = "accel-cmd"
	}
	return &Client{run: run, bin: bin}
}

// ShowStat выполняет и разбирает "accel-cmd show stat".
func (c *Client) ShowStat(ctx context.Context) (Stat, error) {
	out, err := c.run.Run(ctx, c.bin, "show", "stat")
	if err != nil {
		return Stat{}, err
	}
	return ParseStat(out)
}

// ParseStat разбирает вывод show stat. Формат — секции с отступом,
// например:
//
//	uptime: 0.12:54:50
//	sessions:
//	  active: 1
//	radius(1, 127.0.0.1):
//	  state: active
//	  fail count: 0
func ParseStat(raw string) (Stat, error) {
	var st Stat
	section := ""
	recognized := false
	mods := make(map[string]*Module, len(moduleOrder))
	for _, name := range moduleOrder {
		mods[name] = &Module{Name: name}
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		indented := line[0] == ' ' || line[0] == '\t'
		trimmed := strings.TrimSpace(line)

		if !indented {
			key, val, ok := splitKV(trimmed)
			if !ok {
				continue
			}
			section = key
			if m, ok := mods[key]; ok {
				m.Present = true
				recognized = true
			}
			switch {
			case key == "uptime":
				st.Uptime = val
				recognized = true
			case key == "cpu":
				st.CPU = val
				recognized = true
			case strings.HasPrefix(key, "mem(rss/virt)"):
				parseMem(val, &st)
				recognized = true
			case strings.HasPrefix(key, "radius"):
				section = "radius"
			}
			continue
		}

		key, val, ok := splitKV(trimmed)
		if !ok {
			continue
		}
		switch section {
		case "sessions":
			switch key {
			case "active":
				st.SessionsActive = atoi(val)
				recognized = true
			case "starting":
				st.SessionsStarting = atoi(val)
				recognized = true
			}
		case "pppoe":
			if key == "active" {
				st.PPPoEActive = atoi(val)
				recognized = true
			}
		case "ipoe":
			if key == "active" {
				st.IPoEActive = atoi(val)
				recognized = true
			}
		case "radius":
			switch key {
			case "state":
				st.RadiusState = val
				recognized = true
			case "fail count":
				st.RadiusFail = atoi(val)
				recognized = true
			}
		}
		// Для l2tp/module-секций active может встречаться на вложенном
		// уровне (tunnels/sessions); берём максимум.
		if m, ok := mods[section]; ok && key == "active" {
			if n := atoi(val); n > m.Active {
				m.Active = n
			}
		}
	}
	if !recognized {
		return Stat{}, fmt.Errorf("accel-cmd show stat: не удалось разобрать вывод")
	}
	for _, name := range moduleOrder {
		st.Modules = append(st.Modules, *mods[name])
	}
	return st, nil
}

func parseMem(val string, st *Stat) {
	// "9736/321048 kB"
	val = strings.TrimSuffix(strings.TrimSpace(val), "kB")
	parts := strings.SplitN(strings.TrimSpace(val), "/", 2)
	if len(parts) != 2 {
		return
	}
	st.RSSKB = int64(atoi(parts[0]))
	st.VirtKB = int64(atoi(parts[1]))
}

// ShowSessions выполняет и разбирает "accel-cmd show sessions".
func (c *Client) ShowSessions(ctx context.Context) ([]Session, error) {
	out, err := c.run.Run(ctx, c.bin, "show", "sessions")
	if err != nil {
		return nil, err
	}
	return ParseSessions(out), nil
}

// ParseSessions разбирает таблицу сессий. Колонки:
// ifname | username | calling-sid | ip | rate-limit | type | comp | state | uptime
func ParseSessions(raw string) []Session {
	var out []Session
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 9 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		// Строка-разделитель состоит из дефисов и плюсов.
		if strings.Trim(cols[0], "-+ ") == "" || strings.EqualFold(cols[0], "ifname") {
			continue
		}
		out = append(out, Session{
			IfName:     cols[0],
			Username:   cols[1],
			CallingSID: cols[2],
			IP:         cols[3],
			RateLimit:  cols[4],
			Type:       cols[5],
			Comp:       cols[6],
			State:      cols[7],
			Uptime:     cols[8],
		})
	}
	return out
}

// TerminateUser мягко завершает сессию абонента.
func (c *Client) TerminateUser(ctx context.Context, login string) (string, error) {
	return c.run.Run(ctx, c.bin, "terminate", "username", login, "soft")
}

// TerminateInterface мягко завершает сессию по интерфейсу.
func (c *Client) TerminateInterface(ctx context.Context, ifname string) (string, error) {
	return c.run.Run(ctx, c.bin, "terminate", "if", ifname, "soft")
}

func splitKV(s string) (key, val string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
