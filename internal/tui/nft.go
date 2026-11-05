package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"accel-menu/internal/nft"
	"accel-menu/internal/service"
)

type nftPage struct {
	root   *tview.Flex
	search *tview.InputField
	sets   *tview.Table
	elems  *tview.Table

	refreshBtn *tview.Button
	flushBtn   *tview.Button
	delBtn     *tview.Button

	setsData   []service.NFTSet
	elemsData  []nft.Element
	suppress   bool
	pendingIP  string
	selectedIP string
}

func newNFTPage(a *App) *nftPage {
	p := &nftPage{}

	p.search = tview.NewInputField().
		SetLabel("Поиск IP: ").
		SetFieldWidth(0)
	p.search.SetBackgroundColor(colBg)
	p.search.SetFieldBackgroundColor(colBg)
	p.search.SetFieldTextColor(colText)
	p.search.SetLabelColor(colWarn)

	p.sets = newTable("СЕТЫ NFT (inet filter)")
	p.sets.SetFixed(1, 0)
	p.sets.SetCell(0, 0, headerCell("SET").SetExpansion(2))
	p.sets.SetCell(0, 1, headerCell("НАЗНАЧЕНИЕ").SetExpansion(5))
	p.sets.SetCell(0, 2, headerCell("ЭЛЕМЕНТОВ").SetExpansion(1))
	p.sets.SetSelectionChangedFunc(func(row, column int) {
		if p.suppress || row <= 0 {
			return
		}
		if name := p.SelectedSet(); name != "" {
			p.selectedIP = ""
			a.loadElements(name)
		}
	})

	p.elems = newTable("СОДЕРЖИМОЕ SET")
	p.elems.SetFixed(1, 0)
	p.elems.SetCell(0, 0, headerCell("IP").SetExpansion(2))
	p.elems.SetCell(0, 1, headerCell("ОСТАТОК TTL").SetExpansion(1))
	p.elems.SetSelectionChangedFunc(func(row, column int) {
		if row > 0 && row-1 < len(p.elemsData) {
			p.selectedIP = p.elemsData[row-1].IP
		}
	})

	p.refreshBtn = newButton("Обновить (F5)")
	p.refreshBtn.SetSelectedFunc(a.loadSets)
	p.flushBtn = newButton("Очистить set (F8)")
	p.flushBtn.SetSelectedFunc(a.confirmFlushSet)
	p.delBtn = newButton("Удалить IP (Del)")
	p.delBtn.SetSelectedFunc(a.confirmDeleteElement)

	buttons := tview.NewFlex().
		AddItem(p.refreshBtn, 0, 1, false).
		AddItem(p.flushBtn, 0, 1, false).
		AddItem(p.delBtn, 0, 1, false)
	buttons.SetBackgroundColor(colBg)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.search, 1, 0, false).
		AddItem(p.sets, 10, 0, true).
		AddItem(p.elems, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	p.root.SetBackgroundColor(colBg)
	return p
}

// SetSets перестраивает список set-ов.
func (p *nftPage) SetSets(sets []service.NFTSet, selected string) {
	oldSelected := p.SelectedSet()
	if selected == "" {
		selected = oldSelected
	}
	p.setsData = sets
	p.suppress = true
	p.sets.Clear()
	p.sets.SetCell(0, 0, headerCell("SET").SetExpansion(2))
	p.sets.SetCell(0, 1, headerCell("НАЗНАЧЕНИЕ").SetExpansion(5))
	p.sets.SetCell(0, 2, headerCell("ЭЛЕМЕНТОВ").SetExpansion(1))

	target := -1
	for i, s := range sets {
		row := i + 1
		nameColor := colText
		if s.Extra {
			nameColor = colTitle
		}
		p.sets.SetCell(row, 0, tview.NewTableCell(s.Name).SetTextColor(nameColor).SetExpansion(2))
		desc := s.Desc
		if s.Extra {
			desc = "[прочий set] " + desc
		}
		p.sets.SetCell(row, 1, tview.NewTableCell(desc).SetTextColor(colDim).SetExpansion(5))
		count := fmt.Sprintf("%d", s.Count)
		countColor := colDim
		if s.Missing {
			count = "—"
			countColor = colErr
		} else if s.Count > 0 {
			countColor = colWarn
		}
		p.sets.SetCell(row, 2, tview.NewTableCell(count).SetTextColor(countColor).SetExpansion(1))
		if s.Name == selected {
			target = row
		}
	}
	if target < 0 && len(sets) > 0 {
		target = 1
	}
	if target > 0 {
		p.sets.Select(target, 0)
	}
	p.suppress = false

	if target > 0 {
		p.elems.SetTitle(" СОДЕРЖИМОЕ: " + sets[target-1].Name + " ")
		if sets[target-1].Name != oldSelected {
			p.selectedIP = ""
		}
	} else {
		p.elems.SetTitle(" СОДЕРЖИМОЕ SET ")
		p.elems.Clear()
		p.elemsData = nil
		p.elems.SetCell(0, 0, headerCell("IP").SetExpansion(2))
		p.elems.SetCell(0, 1, headerCell("ОСТАТОК TTL").SetExpansion(1))
		p.elems.SetCell(1, 0, tview.NewTableCell("(нет наборов)").
			SetTextColor(colDim).SetExpansion(2))
	}
}

// SelectedSet возвращает имя выбранного set-а.
func (p *nftPage) SelectedSet() string {
	row, _ := p.sets.GetSelection()
	if row <= 0 || row-1 >= len(p.setsData) {
		return ""
	}
	return p.setsData[row-1].Name
}

// SelectedSetInfo возвращает выбранный set.
func (p *nftPage) SelectedSetInfo() (service.NFTSet, bool) {
	row, _ := p.sets.GetSelection()
	if row <= 0 || row-1 >= len(p.setsData) {
		return service.NFTSet{}, false
	}
	return p.setsData[row-1], true
}

// SelectedElement возвращает выбранный IP.
func (p *nftPage) SelectedElement() string {
	row, _ := p.elems.GetSelection()
	if row <= 0 || row-1 >= len(p.elemsData) {
		return ""
	}
	return p.elemsData[row-1].IP
}

// SearchQuery возвращает запрос поиска по IP.
func (p *nftPage) SearchQuery() string {
	return strings.TrimSpace(p.search.GetText())
}

// ClearSearch очищает поле поиска IP.
func (p *nftPage) ClearSearch() {
	p.search.SetText("")
}

// SetPendingIP задаёт IP, который нужно выделить после загрузки set-а.
func (p *nftPage) SetPendingIP(ip string) {
	p.pendingIP = ip
}

// SelectSet выбирает set по имени. Возвращает false, если set не найден.
func (p *nftPage) SelectSet(name string) bool {
	if p.SelectedSet() == name {
		return true
	}
	for i, s := range p.setsData {
		if s.Name != name {
			continue
		}
		p.suppress = true
		p.sets.Select(i+1, 0)
		p.suppress = false
		p.selectedIP = ""
		return true
	}
	return false
}

// SetElements рисует содержимое set-а.
func (p *nftPage) SetElements(name string, els []nft.Element, err error) {
	p.elemsData = els
	p.elems.SetTitle(" СОДЕРЖИМОЕ: " + name + " ")
	p.elems.Clear()
	p.elems.SetCell(0, 0, headerCell("IP").SetExpansion(2))
	p.elems.SetCell(0, 1, headerCell("ОСТАТОК TTL").SetExpansion(1))

	if err != nil {
		p.pendingIP = ""
		p.elems.SetCell(1, 0, tview.NewTableCell("ошибка: "+err.Error()).
			SetTextColor(colErr).SetExpansion(2))
		return
	}
	if len(els) == 0 {
		p.pendingIP = ""
		p.elems.SetCell(1, 0, tview.NewTableCell("(пусто)").
			SetTextColor(colDim).SetExpansion(2))
		return
	}
	for i, e := range els {
		row := i + 1
		ttl := e.TTL
		if ttl == "" {
			ttl = "—"
		}
		p.elems.SetCell(row, 0, tview.NewTableCell(e.IP).SetTextColor(colText).SetExpansion(2))
		p.elems.SetCell(row, 1, tview.NewTableCell(ttl).SetTextColor(colWarn).SetExpansion(1))
	}
	desired := p.pendingIP
	if desired == "" {
		desired = p.selectedIP
	}
	selected := false
	if desired != "" {
		for i, e := range els {
			if e.IP == desired {
				p.elems.Select(i+1, 0)
				p.selectedIP = desired
				selected = true
				break
			}
		}
	}
	if !selected {
		p.elems.Select(1, 0)
	}
	p.pendingIP = ""
}

// RenderError показывает ошибку nft вместо списка.
func (p *nftPage) RenderError(err error) {
	p.setsData = nil
	p.elemsData = nil
	p.suppress = true
	p.sets.Clear()
	p.sets.SetCell(0, 0, headerCell("SET").SetExpansion(2))
	p.sets.SetCell(1, 0, tview.NewTableCell("ошибка nft: "+err.Error()).
		SetTextColor(colErr).SetExpansion(2))
	p.suppress = false
	p.elems.Clear()
	p.elems.SetCell(0, 0, headerCell("IP").SetExpansion(2))
	p.elems.SetCell(1, 0, tview.NewTableCell("—").SetTextColor(colDim).SetExpansion(1))
}

// HintTitle — описание выбранного set-а для заголовка.
func (p *nftPage) HintTitle() string {
	if s, ok := p.SelectedSetInfo(); ok {
		return fmt.Sprintf(" %s — %s ", s.Name, strings.TrimSpace(s.Desc))
	}
	return " СОДЕРЖИМОЕ SET "
}
