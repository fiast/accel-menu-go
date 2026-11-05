// Package nft читает и изменяет nftables: set-ы блокировок и NAT-правила.
package nft

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"accel-menu/internal/execx"
)

// Set — nftables set с количеством элементов.
type Set struct {
	Name     string
	Count    int
	Flags    []string
	Elements []string
}

// Element — IP-адрес внутри set-а и остаток TTL (если nft его отдаёт).
type Element struct {
	IP  string
	TTL string
}

// Client — клиент nft.
type Client struct {
	run    *execx.Runner
	family string
	table  string
}

// New создаёт клиент.
func New(run *execx.Runner, family, table string) *Client {
	if family == "" {
		family = "inet"
	}
	if table == "" {
		table = "filter"
	}
	return &Client{run: run, family: family, table: table}
}

// Table возвращает "family table".
func (c *Client) Table() string { return c.family + " " + c.table }

// Sets возвращает все set-ы таблицы одним вызовом nft -j.
func (c *Client) Sets(ctx context.Context) ([]Set, error) {
	out, err := c.run.RunSudo(ctx, "nft", "-j", "list", "table", c.family, c.table)
	if err != nil {
		return nil, err
	}
	return ParseSetsJSON(out)
}

// ParseSetsJSON разбирает вывод "nft -j list table ...".
func ParseSetsJSON(raw string) ([]Set, error) {
	var doc struct {
		Nftables []json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("nft json: %w", err)
	}
	var sets []Set
	for _, item := range doc.Nftables {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		setRaw, ok := obj["set"]
		if !ok {
			continue
		}
		var s struct {
			Name  string            `json:"name"`
			Flags []string          `json:"flags"`
			Elem  []json.RawMessage `json:"elem"`
		}
		if err := json.Unmarshal(setRaw, &s); err != nil {
			continue
		}
		if s.Name == "" {
			continue
		}
		set := Set{Name: s.Name, Count: len(s.Elem), Flags: s.Flags}
		for _, raw := range s.Elem {
			if ip := elemIP(raw); ip != "" {
				set.Elements = append(set.Elements, ip)
			}
		}
		sets = append(sets, set)
	}
	return sets, nil
}

var elemIPRe = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}`)

// elemIP извлекает IP из элемента nft JSON (строка или объект с val).
func elemIP(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Val  string `json:"val"`
		Elem struct {
			Val string `json:"val"`
		} `json:"elem"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Val != "" {
			return obj.Val
		}
		if obj.Elem.Val != "" {
			return obj.Elem.Val
		}
	}
	return elemIPRe.FindString(string(raw))
}

// Elements возвращает элементы set-а с TTL.
func (c *Client) Elements(ctx context.Context, name string) ([]Element, error) {
	if !ValidSetName(name) {
		return nil, fmt.Errorf("недопустимое имя set-а %q", name)
	}
	out, err := c.run.RunSudo(ctx, "nft", "list", "set", c.family, c.table, name)
	if err != nil {
		return nil, err
	}
	return ParseElements(out), nil
}

// ParseElements разбирает текстовый вывод "nft list set".
//
// Формат:
//
//	set ipset_1 {
//	    type ipv4_addr
//	    flags timeout
//	    elements = { 1.2.3.4 expires 4m58s,
//	                 5.6.7.8 }
//	}
func ParseElements(raw string) []Element {
	start := strings.Index(raw, "elements = {")
	if start < 0 {
		return nil
	}
	rest := raw[start+len("elements = {"):]
	end := strings.Index(rest, "}")
	if end < 0 {
		end = len(rest)
	}
	block := rest[:end]
	var out []Element
	for _, chunk := range strings.Split(block, ",") {
		fields := strings.Fields(chunk)
		if len(fields) == 0 {
			continue
		}
		ip := strings.Trim(fields[0], "{} \t\r\n")
		if net.ParseIP(ip) == nil {
			continue
		}
		e := Element{IP: ip}
		hasExpires := false
		for i := 1; i+1 < len(fields); i++ {
			switch fields[i] {
			case "expires":
				e.TTL = fields[i+1]
				hasExpires = true
			case "timeout":
				if e.TTL == "" {
					e.TTL = fields[i+1]
				}
			}
		}
		if hasExpires {
			// nft печатает остаток как "4m59s992ms" — округляем до секунд.
			if d, err := time.ParseDuration(e.TTL); err == nil {
				e.TTL = d.Truncate(time.Second).String()
			}
		}
		out = append(out, e)
	}
	return out
}

// Flush очищает set.
func (c *Client) Flush(ctx context.Context, name string) error {
	if !ValidSetName(name) {
		return fmt.Errorf("недопустимое имя set-а %q", name)
	}
	_, err := c.run.RunSudo(ctx, "nft", "flush", "set", c.family, c.table, name)
	return err
}

// DeleteElement удаляет один IP из set-а.
func (c *Client) DeleteElement(ctx context.Context, name, ip string) error {
	if !ValidSetName(name) {
		return fmt.Errorf("недопустимое имя set-а %q", name)
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("недопустимый IP %q", ip)
	}
	_, err := c.run.RunSudo(ctx, "nft", "delete", "element", c.family, c.table, name, "{", ip, "}")
	return err
}

// Postrouting возвращает текст chain postrouting (NAT-правила).
func (c *Client) Postrouting(ctx context.Context) (string, error) {
	return c.run.RunSudo(ctx, "nft", "list", "chain", c.family, c.table, "postrouting")
}

var masqRe = regexp.MustCompile(`oifname\s+"([^"]+)"\s+masquerade`)

// MasqueradeInterfaces извлекает интерфейсы из правил masquerade.
func MasqueradeInterfaces(text string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range masqRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// HasNATFor проверяет наличие правила masquerade для подсети.
func HasNATFor(text, cidr string) bool {
	if !strings.Contains(text, cidr) {
		return false
	}
	return strings.Contains(text, "masquerade")
}

// MatchNATFor возвращает строки правил, относящиеся к подсети.
func MatchNATFor(text, cidr string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, cidr) && strings.Contains(line, "masquerade") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

var setNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// ValidSetName проверяет имя set-а перед подстановкой в аргументы nft.
func ValidSetName(name string) bool {
	return len(name) > 0 && len(name) <= 64 && setNameRe.MatchString(name)
}
