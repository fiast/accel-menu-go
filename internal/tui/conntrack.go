package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"accel-menu/internal/accel"
	"accel-menu/internal/procfs"
	"accel-menu/internal/service"
)

type ctSample struct {
	orig, reply uint64
}

type conntrackPage struct {
	root   *tview.Flex
	search *tview.InputField
	table  *tview.Table

	target   accel.Session
	all      []service.ConntrackEntry
	filtered []service.ConntrackEntry
	query    string

	prev   map[string]ctSample
	prevAt time.Time
}

func newConntrackPage() *conntrackPage {
	p := &conntrackPage{prev: make(map[string]ctSample)}

	p.search = tview.NewInputField().
		SetLabel("Поиск: ").
		SetFieldWidth(0)
	p.search.SetBackgroundColor(colBg)
	p.search.SetFieldBackgroundColor(colBg)
	p.search.SetFieldTextColor(colText)
	p.search.SetLabelColor(colWarn)
	p.search.SetChangedFunc(func(text string) {
		p.query = text
		p.apply("")
	})

	p.table = newTable("CONNTRACK")
	p.table.SetFixed(1, 0)

	headers := []struct {
		title string
		exp   int
	}{
		{"ST", 1}, {"SRC", 2}, {"SPORT", 1}, {"DST", 2}, {"DPORT", 1},
		{"REPLY SRC", 2}, {"REPLY DST", 2}, {"PROTO", 1}, {"TIMEOUT", 1},
		{"TCP STATE", 1}, {"ORIG RATE", 1}, {"REPL RATE", 1},
		{"ORIG BYTES", 1}, {"REPL BYTES", 1},
	}
	for i, h := range headers {
		p.table.SetCell(0, i, headerCell(h.title).SetExpansion(h.exp))
	}

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.search, 1, 0, false).
		AddItem(p.table, 0, 1, true)
	p.root.SetBackgroundColor(colBg)
	return p
}

func (p *conntrackPage) Open(s accel.Session) {
	p.target = s
	p.query = ""
	p.search.SetText("")
	p.all = nil
	p.filtered = nil
	p.prev = make(map[string]ctSample)
	p.prevAt = time.Time{}
	p.table.SetTitle(fmt.Sprintf(" CONNTRACK: %s — %s ", s.Username, s.IP))
	p.appliedTitle()
}

func (p *conntrackPage) appliedTitle() {
	if p.target.IP == "" {
		return
	}
	if p.query == "" {
		p.table.SetTitle(fmt.Sprintf(" CONNTRACK: %s — %s — %d ", p.target.Username, p.target.IP, len(p.filtered)))
	} else {
		p.table.SetTitle(fmt.Sprintf(" CONNTRACK: %s — %s — найдено %d из %d: %s ",
			p.target.Username, p.target.IP, len(p.filtered), len(p.all), p.query))
	}
}

func (p *conntrackPage) Render(rows []service.ConntrackEntry, err error) {
	if err != nil {
		p.all = nil
		p.filtered = nil
		p.table.Clear()
		p.table.SetCell(0, 0, headerCell("ОШИБКА").SetExpansion(14))
		p.table.SetCell(1, 0, tview.NewTableCell("conntrack: "+err.Error()).
			SetTextColor(colErr).SetExpansion(14))
		p.table.SetTitle(fmt.Sprintf(" CONNTRACK: %s — %s ", p.target.Username, p.target.IP))
		return
	}
	now := time.Now()
	dt := 0.0
	if !p.prevAt.IsZero() {
		dt = now.Sub(p.prevAt).Seconds()
	}
	next := make(map[string]ctSample, len(rows))
	for i := range rows {
		e := &rows[i]
		if prev, ok := p.prev[e.Key]; ok && dt > 0 {
			if e.OrigBytes >= prev.orig {
				e.OrigRate = float64(e.OrigBytes-prev.orig) / dt
			}
			if e.ReplyBytes >= prev.reply {
				e.ReplyRate = float64(e.ReplyBytes-prev.reply) / dt
			}
		}
		next[e.Key] = ctSample{orig: e.OrigBytes, reply: e.ReplyBytes}
	}
	p.prev = next
	p.prevAt = now
	p.all = rows
	p.apply("")
}

func (p *conntrackPage) apply(keep string) {
	if keep == "" {
		keep = p.SelectedKey()
	}
	q := strings.ToLower(strings.TrimSpace(p.query))
	p.filtered = p.filtered[:0]
	for _, e := range p.all {
		if q == "" || conntrackContains(e, q) {
			p.filtered = append(p.filtered, e)
		}
	}

	p.table.Clear()
	headers := []struct {
		title string
		exp   int
	}{
		{"ST", 1}, {"SRC", 2}, {"SPORT", 1}, {"DST", 2}, {"DPORT", 1},
		{"REPLY SRC", 2}, {"REPLY DST", 2}, {"PROTO", 1}, {"TIMEOUT", 1},
		{"TCP STATE", 1}, {"ORIG RATE", 1}, {"REPL RATE", 1},
		{"ORIG BYTES", 1}, {"REPL BYTES", 1},
	}
	for i, h := range headers {
		p.table.SetCell(0, i, headerCell(h.title).SetExpansion(h.exp))
	}
	target := -1
	for i, e := range p.filtered {
		row := i + 1
		p.table.SetCell(row, 0, tview.NewTableCell(e.Status).SetTextColor(colWarn).SetExpansion(1))
		p.table.SetCell(row, 1, textCell(orDash(e.Src)))
		p.table.SetCell(row, 2, tview.NewTableCell(portText(e.SrcPort)).SetTextColor(colDim).SetExpansion(1))
		p.table.SetCell(row, 3, textCell(orDash(e.Dst)))
		p.table.SetCell(row, 4, tview.NewTableCell(portText(e.DstPort)).SetTextColor(colDim).SetExpansion(1))
		p.table.SetCell(row, 5, textCell(orDash(e.ReplySrc)))
		p.table.SetCell(row, 6, textCell(orDash(e.ReplyDst)))
		p.table.SetCell(row, 7, tview.NewTableCell(fmt.Sprintf("%d (%s)", e.ProtoNum, e.Proto)).SetExpansion(1))
		p.table.SetCell(row, 8, tview.NewTableCell(procfs.FormatTimeout(e.Timeout)).SetTextColor(colDim).SetExpansion(1))
		p.table.SetCell(row, 9, tview.NewTableCell(orDash(e.TCPState)).SetTextColor(colDim).SetExpansion(1))
		p.table.SetCell(row, 10, tview.NewTableCell(formatRate(e.OrigRate)).SetTextColor(colText).SetExpansion(1))
		p.table.SetCell(row, 11, tview.NewTableCell(formatRate(e.ReplyRate)).SetTextColor(colText).SetExpansion(1))
		p.table.SetCell(row, 12, tview.NewTableCell(formatBytes(float64(e.OrigBytes))).SetExpansion(1))
		p.table.SetCell(row, 13, tview.NewTableCell(formatBytes(float64(e.ReplyBytes))).SetExpansion(1))
		if e.Key == keep {
			target = row
		}
	}
	if target < 0 && len(p.filtered) > 0 {
		target = 1
	}
	if target > 0 {
		p.table.Select(target, 0)
	}
	if len(p.filtered) == 0 {
		msg := "(conntrack пуст)"
		if q != "" {
			msg = "(ничего не найдено)"
		}
		p.table.SetCell(1, 0, tview.NewTableCell(msg).SetTextColor(colDim).SetExpansion(14))
	}
	p.appliedTitle()
}

func conntrackContains(e service.ConntrackEntry, q string) bool {
	hay := strings.ToLower(strings.Join([]string{
		e.Status, e.Src, portText(e.SrcPort), e.Dst, portText(e.DstPort),
		e.ReplySrc, e.ReplyDst, e.Proto, procfs.FormatTimeout(e.Timeout),
		e.TCPState, fmt.Sprintf("%d", e.ProtoNum),
	}, " "))
	return strings.Contains(hay, q)
}

func portText(v int) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d", v)
}

func (p *conntrackPage) SelectedKey() string {
	row, _ := p.table.GetSelection()
	if row <= 0 || row-1 >= len(p.filtered) {
		return ""
	}
	return p.filtered[row-1].Key
}

func (p *conntrackPage) FindNext(delta int) (key string, idx, total int) {
	total = len(p.filtered)
	if total == 0 {
		return "", 0, 0
	}
	row, _ := p.table.GetSelection()
	i := row - 1
	if i < 0 || i >= total {
		i = 0
	}
	i = (i + delta + total) % total
	p.table.Select(i+1, 0)
	return p.filtered[i].Key, i + 1, total
}

func (p *conntrackPage) ClearSearch() {
	p.query = ""
	p.search.SetText("")
	p.apply("")
	p.table.Select(1, 0)
}
