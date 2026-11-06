package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type logsPage struct {
	root *tview.Flex
	// search — поле поиска по строкам журнала.
	search *tview.InputField
	view   *tview.TextView
	bar    *scrollbar
	title  string
	follow bool

	all      []string
	filtered []string
	query    string
}

func newLogsPage() *logsPage {
	return newLogsPageTitle("ЖУРНАЛ ACCEL-PPP", true)
}

func newLogsPageTitle(title string, follow bool) *logsPage {
	p := &logsPage{title: title, follow: follow}

	p.search = tview.NewInputField().
		SetLabel("Поиск: ").
		SetFieldWidth(0)
	p.search.SetBackgroundColor(colBg)
	p.search.SetFieldBackgroundColor(colBg)
	p.search.SetFieldTextColor(colText)
	p.search.SetLabelColor(colWarn)
	p.search.SetChangedFunc(func(text string) {
		p.query = text
		p.apply(false)
		p.view.ScrollToBeginning()
	})

	p.view = newPanel(title)
	p.view.SetScrollable(true)
	p.view.SetWrap(false)
	p.view.SetText("[gray]загрузка…[-]")

	p.bar = newScrollbar(p.view)
	body := tview.NewFlex().
		AddItem(p.view, 0, 1, true).
		AddItem(p.bar, 1, 0, false)
	body.SetBackgroundColor(colBg)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.search, 1, 0, false).
		AddItem(body, 0, 1, true)
	p.root.SetBackgroundColor(colBg)
	return p
}

// Render принимает свежий хвост лога и перерисовывает его с учётом поиска.
func (p *logsPage) Render(lines []string, err error) {
	if err != nil {
		p.all = nil
		p.filtered = nil
		p.view.SetText("[red]не удалось прочитать: " + tview.Escape(err.Error()) + "[-]")
		p.view.SetTitle(" " + p.title + " ")
		p.bar.SetTotal(0)
		return
	}
	p.all = lines
	p.apply(p.follow)
}

func (p *logsPage) apply(follow bool) {
	q := strings.TrimSpace(p.query)
	lowerQ := strings.ToLower(q)
	filtered := make([]string, 0, len(p.all))
	for _, line := range p.all {
		if q == "" || strings.Contains(strings.ToLower(line), lowerQ) {
			filtered = append(filtered, line)
		}
	}
	p.filtered = filtered

	row, _ := p.view.GetScrollOffset()
	_, _, _, height := p.view.GetInnerRect()
	oldLines := strings.Count(p.view.GetText(true), "\n") + 1
	atBottom := oldLines <= 1 || row+height+1 >= oldLines

	var b strings.Builder
	if len(filtered) == 0 {
		if q == "" {
			b.WriteString("[gray](лог пуст)[-]")
		} else {
			b.WriteString("[gray]ничего не найдено[-]")
		}
	} else {
		for _, line := range filtered {
			if q == "" {
				b.WriteString(colorizeLogLine(line))
			} else {
				b.WriteString(highlightLogLine(line, q))
			}
			b.WriteByte('\n')
		}
	}
	p.view.SetText(b.String())
	p.bar.SetTotal(len(filtered))

	if q == "" {
		p.view.SetTitle(fmt.Sprintf(" %s — %d строк ", p.title, len(filtered)))
	} else {
		p.view.SetTitle(fmt.Sprintf(" %s — найдено %d из %d: %s ", p.title, len(filtered), len(p.all), q))
	}
	if follow && atBottom {
		p.view.ScrollToEnd()
	}
}

// ClearSearch сбрасывает поиск и возвращает полный список строк.
func (p *logsPage) ClearSearch() {
	p.query = ""
	p.search.SetText("")
	p.apply(false)
	p.view.ScrollToBeginning()
}

// RenderError показывает ошибку вместо журнала.
func (p *logsPage) RenderError(err error) {
	p.view.SetText("[red]ошибка: " + tview.Escape(err.Error()) + "[-]")
}

// logBaseColor — цветовая метка для строки до подсветки поиска.
func logBaseColor(line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "access-reject"),
		strings.Contains(low, "authentication failed"),
		strings.Contains(low, "authnak"),
		strings.Contains(low, "failed to authenticate"):
		return "[red]"
	case strings.Contains(low, "warn:"):
		return "[yellow]"
	case strings.Contains(low, "debug:"):
		return "[silver]"
	default:
		return ""
	}
}

// highlightLogLine подсвечивает все вхождения запроса жёлтым, сохраняя
// базовый цвет строки (красный для отказов и т.п.).
func highlightLogLine(line, query string) string {
	q := strings.ToLower(query)
	if q == "" {
		return colorizeLogLine(line)
	}
	lower := strings.ToLower(line)
	base := logBaseColor(line)
	var b strings.Builder
	if base != "" {
		b.WriteString(base)
	}
	pos := 0
	for pos <= len(line) {
		i := strings.Index(lower[pos:], q)
		if i < 0 {
			b.WriteString(tview.Escape(line[pos:]))
			break
		}
		i += pos
		b.WriteString(tview.Escape(line[pos:i]))
		b.WriteString("[yellow]")
		b.WriteString(tview.Escape(line[i : i+len(q)]))
		b.WriteString("[-]")
		if base != "" {
			b.WriteString(base)
		}
		pos = i + len(q)
	}
	if base != "" {
		b.WriteString("[-]")
	}
	return b.String()
}

// scrollbar — вертикальная полоса прокрутки для tview.TextView.
type scrollbar struct {
	*tview.Box
	view  *tview.TextView
	total int
}

func newScrollbar(view *tview.TextView) *scrollbar {
	s := &scrollbar{Box: tview.NewBox(), view: view}
	s.Box.SetBackgroundColor(colBg)
	return s
}

// SetTotal задаёт общее число строк в просматриваемом тексте.
func (s *scrollbar) SetTotal(n int) { s.total = n }

// Draw рисует дорожку и бегунок. Позиция берётся из TextView при каждой
// перерисовке, поэтому прокрутка клавишами сразу отражается на полосе.
func (s *scrollbar) Draw(screen tcell.Screen) {
	s.Box.DrawForSubclass(screen, s)
	x, y, w, h := s.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	track := tcell.StyleDefault.Foreground(colDim).Background(colBg)
	thumb := tcell.StyleDefault.Foreground(colWarn).Background(colBg).Attributes(tcell.AttrBold)
	for i := 0; i < h; i++ {
		screen.SetContent(x, y+i, '│', nil, track)
	}
	if s.total <= 0 {
		return
	}
	_, _, _, viewport := s.view.GetInnerRect()
	if viewport <= 0 {
		viewport = h
	}
	row, _ := s.view.GetScrollOffset()
	pos, size := scrollbarThumb(s.total, viewport, row, h)
	for i := 0; i < size; i++ {
		screen.SetContent(x, y+pos+i, '█', nil, thumb)
	}
}

// scrollbarThumb вычисляет позицию и размер бегунка.
func scrollbarThumb(total, viewport, row, height int) (pos, size int) {
	if height <= 0 || total <= 0 {
		return 0, 0
	}
	if viewport <= 0 {
		viewport = height
	}
	size = height * viewport / total
	if size < 1 {
		size = 1
	}
	if size > height {
		size = height
	}
	maxPos := height - size
	if total <= viewport {
		return 0, height
	}
	pos = row * maxPos / (total - viewport)
	if pos < 0 {
		pos = 0
	}
	if pos > maxPos {
		pos = maxPos
	}
	return pos, size
}
