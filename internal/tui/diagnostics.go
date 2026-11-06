package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"accel-menu/internal/service"
)

type diagPage struct {
	root  *tview.Flex
	table *tview.Table

	reloadBtn  *tview.Button
	restartBtn *tview.Button
	hardBtn    *tview.Button
	confBtn    *tview.Button
	refreshBtn *tview.Button
}

func newDiagPage(a *App) *diagPage {
	t := newTable("ДИАГНОСТИКА")
	t.SetFixed(1, 0)
	t.SetCell(0, 0, headerCell("ПРОВЕРКА").SetExpansion(3))
	t.SetCell(0, 1, headerCell("СТАТУС").SetExpansion(1))
	t.SetCell(0, 2, headerCell("ДЕТАЛИ").SetExpansion(8))

	reload := newButton("Reload (R)")
	reload.SetSelectedFunc(a.confirmReload)
	restart := newButton("Restart (S)")
	restart.SetSelectedFunc(a.confirmRestart)
	hard := newButton("Hard restart (K)")
	hard.SetSelectedFunc(a.confirmHardRestart)
	conf := newButton("Конфиг (C)")
	conf.SetSelectedFunc(a.openAccelConfig)
	refresh := newButton("Обновить (F5)")
	refresh.SetSelectedFunc(a.loadDiag)

	buttons := tview.NewFlex().
		AddItem(conf, 0, 1, false).
		AddItem(reload, 0, 1, false).
		AddItem(restart, 0, 1, false).
		AddItem(hard, 0, 1, false).
		AddItem(refresh, 0, 1, false)
	buttons.SetBackgroundColor(colBg)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	root.SetBackgroundColor(colBg)

	return &diagPage{root: root, table: t, reloadBtn: reload, restartBtn: restart, hardBtn: hard, confBtn: conf, refreshBtn: refresh}
}

func (p *diagPage) Render(checks []service.Check) {
	p.table.Clear()
	p.table.SetCell(0, 0, headerCell("ПРОВЕРКА").SetExpansion(3))
	p.table.SetCell(0, 1, headerCell("СТАТУС").SetExpansion(1))
	p.table.SetCell(0, 2, headerCell("ДЕТАЛИ").SetExpansion(8))

	if len(checks) == 0 {
		p.table.SetCell(1, 0, tview.NewTableCell("нет данных").SetTextColor(colDim))
		return
	}
	ok, warn, fail := service.CountsByStatus(checks)

	for i, c := range checks {
		row := i + 1
		color := colOK
		switch c.Status {
		case service.Warn:
			color = colWarn
		case service.Fail:
			color = colErr
		}
		p.table.SetCell(row, 0, tview.NewTableCell(c.Name).SetTextColor(colText).SetExpansion(3))
		p.table.SetCell(row, 1, tview.NewTableCell(c.Status.Label()).
			SetTextColor(color).
			SetAttributes(tcell.AttrBold).
			SetExpansion(1))
		p.table.SetCell(row, 2, tview.NewTableCell(service.TrimDetail(c.Detail)).
			SetTextColor(colDim).
			SetExpansion(8))
	}

	row := len(checks) + 2
	p.table.SetCell(row, 0, tview.NewTableCell(
		fmt.Sprintf("ИТОГО: OK %d, WARN %d, FAIL %d", ok, warn, fail)).
		SetTextColor(colTitle).
		SetExpansion(9))
}
