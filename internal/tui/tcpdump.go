package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"accel-menu/internal/accel"
)

const tcpdumpMaxLines = 2000

type tcpdumpPage struct {
	root  *tview.Flex
	view  *tview.TextView
	bar   *scrollbar
	lines []string
}

func newTcpdumpPage() *tcpdumpPage {
	p := &tcpdumpPage{}
	p.view = newPanel("TCPDUMP")
	p.view.SetScrollable(true)
	p.view.SetWrap(false)
	p.bar = newScrollbar(p.view)
	body := tview.NewFlex().
		AddItem(p.view, 0, 1, true).
		AddItem(p.bar, 1, 0, false)
	body.SetBackgroundColor(colBg)
	p.root = body
	return p
}

func (p *tcpdumpPage) Reset(s accel.Session) {
	p.lines = nil
	filter := ""
	if s.IP != "" {
		filter = " host " + s.IP
	}
	p.view.SetTitle(fmt.Sprintf(" TCPDUMP: %s%s — Esc остановить ", s.IfName, filter))
	p.view.SetText("[gray]запуск tcpdump…[-]")
	p.bar.SetTotal(0)
}

func (p *tcpdumpPage) AddLine(line string) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	p.lines = append(p.lines, line)
	if len(p.lines) > tcpdumpMaxLines {
		p.lines = p.lines[len(p.lines)-tcpdumpMaxLines:]
	}
	p.view.SetText(tview.Escape(strings.Join(p.lines, "\n")))
	p.bar.SetTotal(len(p.lines))
	p.view.ScrollToEnd()
}

func (p *tcpdumpPage) Notice(text string) {
	p.AddLine(text)
}

func (p *tcpdumpPage) SelectedText() string { return p.view.GetText(true) }
