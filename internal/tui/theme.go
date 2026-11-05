package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Палитра в стиле DOS/FoxPro: тёмно-синий фон, жёлтые рамки,
// белый текст, зелёный успех, красная ошибка.
var (
	colBg     = tcell.NewHexColor(0x000080)
	colBgBar  = tcell.NewHexColor(0x0000A8)
	colBorder = tcell.NewHexColor(0xFFFF00)
	colTitle  = tcell.NewHexColor(0x00FFFF)
	colText   = tcell.ColorWhite
	colDim    = tcell.NewHexColor(0xC0C0C0)
	colOK     = tcell.NewHexColor(0xFFFFFF) // белый: зелёный терялся на голубом фоне
	colErr    = tcell.NewHexColor(0xFF5555)
	colWarn   = tcell.NewHexColor(0xFFFF00)
	colSelect = tcell.NewHexColor(0xFFFF00) // яркий фон выбранной строки
)

// newTextView создаёт TextView с фоном приложения.
func newTextView() *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBackgroundColor(colBg)
	tv.SetTextColor(colText)
	return tv
}

// newPanel — панель с жёлтой рамкой и голубым заголовком.
func newPanel(title string) *tview.TextView {
	tv := newTextView()
	tv.SetBorder(true)
	tv.SetBorderColor(colBorder)
	tv.SetTitle(" " + title + " ")
	tv.SetTitleColor(colTitle)
	tv.SetWrap(false)
	return tv
}

// newTable — таблица с рамкой, заголовком и фиксированной строкой заголовка.
func newTable(title string) *tview.Table {
	t := tview.NewTable()
	t.SetBorders(false)
	t.SetBackgroundColor(colBg)
	t.SetBorder(true)
	t.SetBorderColor(colBorder)
	t.SetTitle(" " + title + " ")
	t.SetTitleColor(colTitle)
	t.SetSelectable(true, false)
	t.SetFixed(1, 0)
	t.SetSelectedStyle(tcell.StyleDefault.
		Foreground(colBg).
		Background(colSelect).
		Attributes(tcell.AttrBold))
	return t
}

// newButton — кнопка в цветах темы.
func newButton(label string) *tview.Button {
	b := tview.NewButton(label)
	b.SetBackgroundColor(colBg)
	b.SetLabelColor(colText)
	b.SetBackgroundColorActivated(colWarn)
	b.SetLabelColorActivated(colBg)
	return b
}

// headerCell — ячейка заголовка таблицы.
func headerCell(text string) *tview.TableCell {
	return tview.NewTableCell(text).
		SetTextColor(colWarn).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false).
		SetExpansion(1)
}

// textCell — обычная ячейка.
func textCell(text string) *tview.TableCell {
	return tview.NewTableCell(text).SetTextColor(colText).SetExpansion(1)
}

// centered размещает p по центру экрана с фиксированными размерами.
func centered(p tview.Primitive, width, height int) tview.Primitive {
	rows := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(p, height, 1, true).
		AddItem(nil, 0, 1, false)
	cols := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(rows, width, 1, true).
		AddItem(nil, 0, 1, false)
	return cols
}

// focusRing — порядок обхода фокуса по Tab/Shift+Tab.
type focusRing struct {
	items []tview.Primitive
	index int
}

func newFocusRing(items ...tview.Primitive) *focusRing {
	return &focusRing{items: items}
}

func (r *focusRing) indexOf(p tview.Primitive) int {
	for i, item := range r.items {
		if item == p {
			return i
		}
	}
	return -1
}
