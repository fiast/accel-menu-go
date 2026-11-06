package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"accel-menu/internal/accel"
)

type sessionsPage struct {
	root   *tview.Flex
	search *tview.InputField
	table  *tview.Table

	bmonBtn    *tview.Button
	tcpdumpBtn *tview.Button
	ctBtn      *tview.Button
	killBtn    *tview.Button
	refreshBtn *tview.Button

	all      []accel.Session
	filtered []accel.Session
	query    string
	suppress bool
}

func newSessionsPage(a *App) *sessionsPage {
	p := &sessionsPage{}

	p.search = tview.NewInputField().
		SetLabel("Поиск: ").
		SetFieldWidth(0)
	p.search.SetBackgroundColor(colBg)
	p.search.SetFieldBackgroundColor(colBg)
	p.search.SetFieldTextColor(colText)
	p.search.SetLabelColor(colWarn)
	p.search.SetChangedFunc(func(text string) {
		p.query = text
		p.apply("", true)
	})

	p.table = newTable("СЕССИИ ACCEL-PPP")
	p.table.SetFixed(1, 0)
	// Колонки тоже выбираемы: на узком терминале Left/Right прокручивают
	// таблицу по горизонтали, чтобы добраться до UPTIME/STATE.
	p.table.SetSelectable(true, true)
	headers := []struct {
		title string
		exp   int
	}{
		{"IFNAME", 2}, {"USERNAME", 2}, {"CALLING-SID", 2}, {"IP", 1},
		{"RATE-LIMIT", 1}, {"TYPE", 1}, {"COMP", 1}, {"STATE", 1}, {"UPTIME", 1},
	}
	for i, h := range headers {
		p.table.SetCell(0, i, headerCell(h.title).SetExpansion(h.exp))
	}

	p.bmonBtn = newButton("bmon (B)")
	p.bmonBtn.SetSelectedFunc(a.runBmon)
	p.tcpdumpBtn = newButton("tcpdump (T)")
	p.tcpdumpBtn.SetSelectedFunc(a.runTcpdump)
	p.ctBtn = newButton("conntrack (C)")
	p.ctBtn.SetSelectedFunc(a.openConntrackSelected)
	p.killBtn = newButton("Прервать (Del)")
	p.killBtn.SetSelectedFunc(a.confirmTerminateSession)
	p.refreshBtn = newButton("Обновить (F5)")
	p.refreshBtn.SetSelectedFunc(a.loadSessions)

	buttons := tview.NewFlex().
		AddItem(p.bmonBtn, 0, 1, false).
		AddItem(p.tcpdumpBtn, 0, 1, false).
		AddItem(p.ctBtn, 0, 1, false).
		AddItem(p.killBtn, 0, 1, false).
		AddItem(p.refreshBtn, 0, 1, false)
	buttons.SetBackgroundColor(colBg)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.search, 1, 0, false).
		AddItem(p.table, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	p.root.SetBackgroundColor(colBg)
	return p
}

// SetSessions обновляет таблицу, сохраняя выбранную сессию.
func (p *sessionsPage) SetSessions(rows []accel.Session, selectedKey string) {
	p.all = rows
	p.apply(selectedKey, false)
}

func (p *sessionsPage) apply(keep string, reset bool) {
	if keep == "" && !reset {
		keep = p.SelectedKey()
	}
	q := strings.ToLower(strings.TrimSpace(p.query))
	_, oldCol := p.table.GetSelection()
	p.filtered = p.filtered[:0]
	for _, s := range p.all {
		if q == "" || sessionContains(s, q) {
			p.filtered = append(p.filtered, s)
		}
	}

	p.suppress = true
	p.table.Clear()
	headers := []struct {
		title string
		exp   int
	}{
		{"IFNAME", 2}, {"USERNAME", 2}, {"CALLING-SID", 2}, {"IP", 1},
		{"RATE-LIMIT", 1}, {"TYPE", 1}, {"COMP", 1}, {"STATE", 1}, {"UPTIME", 1},
	}
	for i, h := range headers {
		p.table.SetCell(0, i, headerCell(h.title).SetExpansion(h.exp))
	}

	target := -1
	for i, s := range p.filtered {
		row := i + 1
		p.table.SetCell(row, 0, textCell(s.IfName))
		p.table.SetCell(row, 1, textCell(s.Username))
		p.table.SetCell(row, 2, tview.NewTableCell(s.CallingSID).SetTextColor(colDim).SetExpansion(2))
		p.table.SetCell(row, 3, textCell(s.IP))
		p.table.SetCell(row, 4, tview.NewTableCell(s.RateLimit).SetTextColor(colWarn).SetExpansion(1))
		typColor := colText
		if strings.EqualFold(s.Type, "ipoe") {
			typColor = colTitle
		}
		p.table.SetCell(row, 5, tview.NewTableCell(s.Type).SetTextColor(typColor).SetExpansion(1))
		p.table.SetCell(row, 6, tview.NewTableCell(s.Comp).SetTextColor(colDim).SetExpansion(1))
		p.table.SetCell(row, 7, tview.NewTableCell(s.State).SetTextColor(colText).SetExpansion(1))
		p.table.SetCell(row, 8, tview.NewTableCell(s.Uptime).SetTextColor(colText).SetExpansion(1))
		if !reset && sessionKey(s) == keep {
			target = row
		}
	}
	if target < 0 && len(p.filtered) > 0 {
		target = 1
	}
	if target > 0 {
		selCol := oldCol
		if reset || selCol < 0 || selCol > 8 {
			selCol = 0
		}
		p.table.Select(target, selCol)
	}
	p.suppress = false

	if len(p.filtered) == 0 {
		if q == "" {
			p.table.SetCell(1, 0, tview.NewTableCell("(активных сессий нет)").SetTextColor(colDim).SetExpansion(9))
		} else {
			p.table.SetCell(1, 0, tview.NewTableCell("(ничего не найдено)").SetTextColor(colDim).SetExpansion(9))
		}
	}
	if q == "" {
		p.table.SetTitle(fmt.Sprintf(" СЕССИИ ACCEL-PPP — %d ", len(p.filtered)))
	} else {
		p.table.SetTitle(fmt.Sprintf(" СЕССИИ ACCEL-PPP — найдено %d из %d: %s ", len(p.filtered), len(p.all), q))
	}
}

func sessionContains(s accel.Session, q string) bool {
	hay := strings.ToLower(strings.Join([]string{
		s.IfName, s.Username, s.CallingSID, s.IP, s.RateLimit, s.Type, s.Comp, s.State, s.Uptime,
	}, " "))
	return strings.Contains(hay, q)
}

func sessionKey(s accel.Session) string {
	return s.IfName + "|" + s.Username + "|" + s.CallingSID
}

// SelectedSession возвращает выбранную сессию.
func (p *sessionsPage) SelectedSession() (accel.Session, bool) {
	row, _ := p.table.GetSelection()
	if row <= 0 || row-1 >= len(p.filtered) {
		return accel.Session{}, false
	}
	return p.filtered[row-1], true
}

// SelectedKey — ключ выбранной сессии.
func (p *sessionsPage) SelectedKey() string {
	s, ok := p.SelectedSession()
	if !ok {
		return ""
	}
	return sessionKey(s)
}

// ClearSearch сбрасывает фильтр.
func (p *sessionsPage) ClearSearch() {
	p.query = ""
	p.search.SetText("")
	p.apply("", false)
	if len(p.filtered) > 0 {
		p.table.Select(1, 0)
	}
}

func (p *sessionsPage) RenderError(err error) {
	p.all = nil
	p.filtered = nil
	p.table.Clear()
	p.table.SetCell(0, 0, headerCell("ОШИБКА").SetExpansion(9))
	p.table.SetCell(1, 0, tview.NewTableCell("accel-cmd: "+err.Error()).
		SetTextColor(colErr).SetExpansion(9))
	p.table.SetTitle(" СЕССИИ ACCEL-PPP ")
}
