package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"accel-menu/internal/store"
)

type subscribersPage struct {
	root        *tview.Flex
	search      *tview.InputField
	table       *tview.Table
	details     *tview.TextView
	createBtn   *tview.Button
	deleteBtn   *tview.Button
	showPassBtn *tview.Button
	refreshBtn  *tview.Button

	rows     []store.SubscriberRow
	filtered []store.SubscriberRow
	query    string
	suppress bool
	onSelect func(string)

	currentLogin string
	lastDetails  store.Details
	reveal       bool
}

func newSubscribersPage(a *App) *subscribersPage {
	p := &subscribersPage{onSelect: a.loadDetails}

	p.search = tview.NewInputField().
		SetLabel("Поиск (логин/MAC/IP): ").
		SetFieldWidth(0)
	p.search.SetBackgroundColor(colBg)
	p.search.SetFieldBackgroundColor(colBg)
	p.search.SetFieldTextColor(colText)
	p.search.SetLabelColor(colWarn)
	p.search.SetChangedFunc(func(text string) {
		p.query = text
		p.apply("", true)
	})

	p.table = newTable("АБОНЕНТЫ")
	p.table.SetFixed(1, 0)
	p.table.SetCell(0, 0, headerCell("ЛОГИН").SetExpansion(2))
	p.table.SetCell(0, 1, headerCell("ТИП").SetExpansion(1))
	p.table.SetCell(0, 2, headerCell("IP").SetExpansion(2))
	p.table.SetSelectionChangedFunc(func(row, column int) {
		if p.suppress || row <= 0 {
			return
		}
		if login := p.loginAt(row); login != "" {
			p.reveal = false
			p.details.SetText("[gray]загрузка карточки…[-]")
			if p.onSelect != nil {
				p.onSelect(login)
			}
		}
	})

	p.details = newPanel("КАРТОЧКА")
	p.details.SetWrap(true)
	p.details.SetScrollable(true)
	p.details.SetText("[gray]выберите абонента в списке слева[-]")

	p.createBtn = newButton("Создать (Ins)")
	p.createBtn.SetSelectedFunc(a.openCreateForm)
	p.deleteBtn = newButton("Удалить (Del)")
	p.deleteBtn.SetSelectedFunc(a.confirmDelete)
	p.showPassBtn = newButton("Пароль (*)")
	p.showPassBtn.SetSelectedFunc(a.togglePassword)
	p.refreshBtn = newButton("Обновить (F5)")
	p.refreshBtn.SetSelectedFunc(func() { a.loadSubs("") })

	buttons := tview.NewFlex().
		AddItem(p.createBtn, 0, 1, false).
		AddItem(p.deleteBtn, 0, 1, false).
		AddItem(p.showPassBtn, 0, 1, false).
		AddItem(p.refreshBtn, 0, 1, false)
	buttons.SetBackgroundColor(colBg)

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.details, 0, 1, true).
		AddItem(buttons, 1, 0, false)

	body := tview.NewFlex().
		AddItem(p.table, 0, 2, true).
		AddItem(right, 0, 3, true)
	body.SetBackgroundColor(colBg)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.search, 1, 0, false).
		AddItem(body, 0, 1, true)
	p.root.SetBackgroundColor(colBg)
	return p
}

// SetRows перестраивает список, сохраняя выбранного абонента.
func (p *subscribersPage) SetRows(rows []store.SubscriberRow, selectLogin string) {
	if selectLogin == "" {
		selectLogin = p.SelectedLogin()
	}
	p.rows = rows
	p.apply(selectLogin, false)
}

func (p *subscribersPage) apply(keep string, reset bool) {
	if keep == "" && !reset {
		keep = p.SelectedLogin()
	}
	q := strings.TrimSpace(p.query)
	p.filtered = p.filtered[:0]
	for _, r := range p.rows {
		if q == "" || subscriberMatches(r, q) {
			p.filtered = append(p.filtered, r)
		}
	}

	p.suppress = true
	p.table.Clear()
	p.table.SetCell(0, 0, headerCell("ЛОГИН").SetExpansion(2))
	p.table.SetCell(0, 1, headerCell("ТИП").SetExpansion(1))
	p.table.SetCell(0, 2, headerCell("IP").SetExpansion(2))

	target := -1
	for i, r := range p.filtered {
		row := i + 1
		typ := "PPPoE"
		typColor := colText
		if r.IsIPoE {
			typ = "IPoE"
			typColor = colTitle
		}
		ip := r.IP
		ipColor := colText
		if ip == "" {
			ip = "—"
			ipColor = colDim
		}
		p.table.SetCell(row, 0, textCell(r.Username))
		p.table.SetCell(row, 1, tview.NewTableCell(typ).SetTextColor(typColor).SetExpansion(1))
		p.table.SetCell(row, 2, tview.NewTableCell(ip).SetTextColor(ipColor).SetExpansion(2))
		if !reset && r.Username == keep {
			target = row
		}
	}
	if target < 0 && len(p.filtered) > 0 {
		target = 1
	}
	if target > 0 {
		p.table.Select(target, 0)
	}
	p.suppress = false

	if len(p.filtered) == 0 {
		if q == "" {
			p.table.SetCell(1, 0, tview.NewTableCell("(абонентов нет)").SetTextColor(colDim).SetExpansion(5))
		} else {
			p.table.SetCell(1, 0, tview.NewTableCell("(ничего не найдено)").SetTextColor(colDim).SetExpansion(5))
		}
	}
	if q == "" {
		p.table.SetTitle(fmt.Sprintf(" АБОНЕНТЫ — %d ", len(p.filtered)))
	} else {
		p.table.SetTitle(fmt.Sprintf(" АБОНЕНТЫ — найдено %d из %d: %s ", len(p.filtered), len(p.rows), q))
	}
	if login := p.SelectedLogin(); login != "" && login != p.currentLogin && p.onSelect != nil {
		p.reveal = false
		p.details.SetText("[gray]загрузка карточки…[-]")
		p.onSelect(login)
	}
}

func subscriberMatches(r store.SubscriberRow, q string) bool {
	ql := strings.ToLower(q)
	if strings.Contains(strings.ToLower(r.Username), ql) {
		return true
	}
	// MAC без разделителей: 02:00:... и 0200... должны находиться одинаково.
	if nq := normalizeMAC(ql); nq != "" && strings.Contains(normalizeMAC(strings.ToLower(r.Username)), nq) {
		return true
	}
	return r.IP != "" && strings.Contains(strings.ToLower(r.IP), ql)
}

func normalizeMAC(s string) string {
	return strings.NewReplacer(":", "", "-", "", ".", "").Replace(s)
}

func (p *subscribersPage) loginAt(row int) string {
	if row <= 0 || row-1 >= len(p.filtered) {
		return ""
	}
	return p.filtered[row-1].Username
}

// SelectedLogin возвращает выбранный логин.
func (p *subscribersPage) SelectedLogin() string {
	row, _ := p.table.GetSelection()
	return p.loginAt(row)
}

// FindNext переключает выбор на следующую/предыдущую найденную строку.
func (p *subscribersPage) FindNext(delta int) (login string, index, total int) {
	total = len(p.filtered)
	if total == 0 {
		return "", 0, 0
	}
	row, _ := p.table.GetSelection()
	i := row - 1
	if i < 0 || i >= total {
		i = 0
	} else {
		i = (i + delta + total) % total
	}
	p.table.Select(i+1, 0)
	return p.filtered[i].Username, i + 1, total
}

// ClearSearch сбрасывает фильтр.
func (p *subscribersPage) ClearSearch() {
	p.query = ""
	p.search.SetText("")
	p.apply("", false)
}

func (p *subscribersPage) RenderError(err error) {
	p.details.SetText("[red]ошибка загрузки списка: " + tview.Escape(err.Error()) + "[-]")
}

// PasswordRevealed сообщает, показан ли пароль.
func (p *subscribersPage) PasswordRevealed() bool { return p.reveal }

// TogglePassword показывает/скрывает пароль в карточке.
func (p *subscribersPage) TogglePassword() {
	if p.currentLogin == "" || p.SelectedLogin() != p.currentLogin {
		return
	}
	p.reveal = !p.reveal
	p.RenderDetails(p.lastDetails, nil)
}

// RenderDetails рисует карточку абонента.
func (p *subscribersPage) RenderDetails(d store.Details, err error) {
	if err != nil {
		p.currentLogin = ""
		p.lastDetails = store.Details{}
		p.reveal = false
		p.details.SetTitle(" КАРТОЧКА ")
		p.details.SetText("[red]ошибка: " + tview.Escape(err.Error()) + "[-]")
		return
	}
	if d.Username != p.currentLogin {
		p.currentLogin = d.Username
		p.reveal = false
	}
	p.lastDetails = d

	typ := "PPPoE"
	if d.IsIPoE {
		typ = "IPoE"
	}
	p.details.SetTitle(" КАРТОЧКА: " + d.Username + " ")

	var b strings.Builder
	fmt.Fprintf(&b, "[cyan]%s[-]   тип: [yellow]%s[-]\n", tview.Escape(d.Username), typ)

	b.WriteString("\n[yellow]ПАРОЛЬ (radcheck)[-]")
	if p.reveal {
		b.WriteString("  [gray](* — скрыть)[-]\n")
	} else {
		b.WriteString("  [gray](* — показать)[-]\n")
	}
	if len(d.Checks) == 0 {
		b.WriteString("  [gray](нет записей)[-]\n")
	}
	for _, c := range d.Checks {
		value := maskValue(c.Attribute, c.Value)
		if p.reveal {
			value = c.Value
		}
		fmt.Fprintf(&b, "  %s = %s\n", tview.Escape(c.Attribute), tview.Escape(value))
	}

	b.WriteString("\n[yellow]АТРИБУТЫ ВЫДАЧИ (radreply)[-]\n")
	if len(d.Replies) == 0 {
		b.WriteString("  [gray](нет — адрес из пула ippool)[-]\n")
	}
	for _, r := range d.Replies {
		fmt.Fprintf(&b, "  %s %s %s\n", tview.Escape(r.Attribute), tview.Escape(r.Op), tview.Escape(r.Value))
	}

	b.WriteString("\n[yellow]ГРУППЫ (radusergroup)[-]\n")
	if len(d.Groups) == 0 {
		b.WriteString("  [gray](нет)[-]\n")
	}
	for _, g := range d.Groups {
		fmt.Fprintf(&b, "  %s (priority %d)\n", tview.Escape(g.Group), g.Priority)
	}

	b.WriteString("\n[yellow]ИСТОРИЯ СЕССИЙ (radacct, последние 5)[-]\n")
	if len(d.Sessions) == 0 {
		b.WriteString("  [gray](сессий не было)[-]\n")
	}
	for _, s := range d.Sessions {
		start := "—"
		if s.Start.Valid {
			start = formatTS(s.Start.Time)
		}
		secs := int64(0)
		if s.Seconds.Valid {
			secs = s.Seconds.Int64
		}
		cause := s.Cause
		if cause == "" {
			cause = "—"
		}
		fmt.Fprintf(&b, "  %s  %-15s  %-8s  %s\n",
			start, tview.Escape(s.IP), formatDuration(secs), tview.Escape(cause))
	}

	b.WriteString("\n[yellow]АВТОРИЗАЦИЯ (radpostauth, последние 5)[-]\n")
	if len(d.Auths) == 0 {
		b.WriteString("  [gray](нет записей)[-]\n")
	}
	for _, a := range d.Auths {
		color := "[white]"
		if strings.Contains(strings.ToLower(a.Reply), "reject") {
			color = "[red]"
		}
		fmt.Fprintf(&b, "  %s  %s%s[-]\n", formatTS(a.Date), color, tview.Escape(a.Reply))
	}

	p.details.SetText(b.String())
}
