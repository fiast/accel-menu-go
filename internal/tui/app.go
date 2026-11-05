// Package tui реализует интерфейс в стиле FoxPro/Turbo Vision.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"accel-menu/internal/accel"
	"accel-menu/internal/config"
	"accel-menu/internal/nft"
	"accel-menu/internal/service"
	"accel-menu/internal/store"
)

const (
	pageDashboard   = "dashboard"
	pageSubscribers = "subscribers"
	pageBlocks      = "blocks"
	pageLogs        = "logs"
	pageSessions    = "sessions"
	pageDiag        = "diagnostics"
	pageModal       = "modal"
)

// Service — то, что TUI нужно от прикладного слоя.
type Service interface {
	Dashboard(context.Context) service.Dashboard
	ListSubscribers(context.Context) ([]store.SubscriberRow, error)
	Details(context.Context, string) (store.Details, error)
	Counts(context.Context, string) (store.Counts, error)
	CreateSubscriber(context.Context, service.NewSubscriberInput) error
	DeleteSubscriber(context.Context, string) (service.DeleteResult, error)
	NFTSets(context.Context) ([]service.NFTSet, error)
	NFTSetElements(context.Context, string) ([]nft.Element, error)
	FindIP(context.Context, string) ([]service.NFTMatch, error)
	FlushSet(context.Context, string) error
	DeleteElement(context.Context, string, string) error
	LogTail(context.Context) ([]string, error)
	Sessions(context.Context) ([]accel.Session, error)
	TerminateSession(context.Context, string) (string, error)
	ReloadAccel(context.Context) (string, error)
	RestartAccel(context.Context) (string, error)
	HardRestartAccel(context.Context) (string, error)
	Diagnostics(context.Context) []service.Check
}

// App — корневое приложение.
type App struct {
	app    *tview.Application
	svc    Service
	cfg    config.Config
	pages  *tview.Pages
	root   *tview.Flex
	status *tview.TextView
	help   *tview.TextView

	dash *dashboardPage
	subs *subscribersPage
	nftP *nftPage
	logs *logsPage
	sess *sessionsPage
	diag *diagPage

	rings map[string]*focusRing

	mu      sync.RWMutex
	current string

	modalOpen bool
	prevFocus tview.Primitive

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	busyDash  atomic.Bool
	busySubs  atomic.Bool
	busySets  atomic.Bool
	busyLogs  atomic.Bool
	busySess  atomic.Bool
	busyDiag  atomic.Bool
	busyFind  atomic.Bool
	detailGen atomic.Uint64
	elemGen   atomic.Uint64

	nftMatches    []service.NFTMatch
	nftMatchIdx   int
	nftMatchQuery string
}

// New собирает TUI.
func New(svc Service, cfg config.Config) *App {
	a := &App{
		app:   tview.NewApplication(),
		svc:   svc,
		cfg:   cfg,
		rings: make(map[string]*focusRing),
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())

	a.status = tview.NewTextView().SetDynamicColors(true)
	a.status.SetBackgroundColor(colBg)
	a.status.SetTextColor(colText)
	a.status.SetWrap(false)

	a.help = tview.NewTextView().SetDynamicColors(true)
	a.help.SetBackgroundColor(colBgBar)
	a.help.SetTextColor(colText)
	a.help.SetWrap(false)

	a.pages = tview.NewPages()
	a.pages.SetBackgroundColor(colBg)

	a.dash = newDashboardPage()
	a.subs = newSubscribersPage(a)
	a.nftP = newNFTPage(a)
	a.logs = newLogsPage()
	a.sess = newSessionsPage(a)
	a.diag = newDiagPage(a)

	a.pages.AddPage(pageDashboard, a.dash.root, true, true)
	a.pages.AddPage(pageSubscribers, a.subs.root, true, false)
	a.pages.AddPage(pageBlocks, a.nftP.root, true, false)
	a.pages.AddPage(pageLogs, a.logs.root, true, false)
	a.pages.AddPage(pageSessions, a.sess.root, true, false)
	a.pages.AddPage(pageDiag, a.diag.root, true, false)

	a.rings[pageSubscribers] = newFocusRing(
		a.subs.table, a.subs.search, a.subs.details,
		a.subs.createBtn, a.subs.deleteBtn, a.subs.showPassBtn, a.subs.refreshBtn,
	)
	a.rings[pageBlocks] = newFocusRing(
		a.nftP.sets, a.nftP.elems, a.nftP.search, a.nftP.refreshBtn, a.nftP.flushBtn, a.nftP.delBtn,
	)
	a.rings[pageLogs] = newFocusRing(a.logs.view, a.logs.search)
	a.rings[pageSessions] = newFocusRing(
		a.sess.table, a.sess.search, a.sess.bmonBtn, a.sess.killBtn, a.sess.refreshBtn,
	)
	a.rings[pageDiag] = newFocusRing(
		a.diag.table, a.diag.reloadBtn, a.diag.restartBtn, a.diag.hardBtn, a.diag.refreshBtn,
	)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false).
		AddItem(a.help, 1, 0, false)
	a.root.SetBackgroundColor(colBg)

	a.app.SetRoot(a.root, true)
	a.app.EnableMouse(false)
	a.app.SetInputCapture(a.inputCapture)

	a.showPage(pageDashboard, false)
	a.setStatus("info", "готово; F1..F4, F6, F9 — экраны, F10/ESC — выход")
	return a
}

// Run запускает event loop.
func (a *App) Run() error {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.loop()
	}()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		select {
		case <-time.After(150 * time.Millisecond):
			a.loadCurrent()
		case <-a.ctx.Done():
		}
	}()

	err := a.app.Run()
	a.cancel()
	a.wg.Wait()
	return err
}

func (a *App) loop() {
	tick := time.NewTicker(a.cfg.Refresh)
	defer tick.Stop()
	slow := time.NewTicker(a.cfg.SlowRefresh)
	defer slow.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			switch a.pageName() {
			case pageDashboard:
				a.loadDashboard()
			case pageLogs:
				a.loadLogs()
			case pageSessions:
				a.loadSessions()
			}
		case <-slow.C:
			if a.pageName() == pageBlocks {
				a.loadSets()
			}
		}
	}
}

func (a *App) inputCapture(ev *tcell.EventKey) *tcell.EventKey {
	if a.modalOpen {
		if ev.Key() == tcell.KeyEscape {
			a.closeModal()
			return nil
		}
		return ev
	}
	key := ev.Key()

	// Глобальные клавиши работают даже когда фокус в поле поиска.
	switch key {
	case tcell.KeyF1:
		a.showPage(pageDashboard, true)
		return nil
	case tcell.KeyF2:
		a.showPage(pageSubscribers, true)
		return nil
	case tcell.KeyF3:
		a.showPage(pageBlocks, true)
		return nil
	case tcell.KeyF4:
		a.showPage(pageLogs, true)
		return nil
	case tcell.KeyF6:
		a.showPage(pageSessions, true)
		return nil
	case tcell.KeyF9:
		a.showPage(pageDiag, true)
		return nil
	case tcell.KeyF5:
		a.loadCurrent()
		return nil
	case tcell.KeyF8:
		if a.pageName() == pageBlocks {
			a.confirmFlushSet()
			return nil
		}
	case tcell.KeyF10:
		a.confirmQuit()
		return nil
	case tcell.KeyTab:
		a.cycleFocus(1)
		return nil
	case tcell.KeyBacktab:
		a.cycleFocus(-1)
		return nil
	case tcell.KeyCtrlF:
		if a.pageName() == pageLogs || a.pageName() == pageSessions || a.pageName() == pageSubscribers || a.pageName() == pageBlocks {
			a.focusSearch()
			return nil
		}
	case tcell.KeyCtrlN:
		a.findNext(1)
		return nil
	case tcell.KeyCtrlP:
		a.findNext(-1)
		return nil
	case tcell.KeyF7:
		if a.pageName() == pageSubscribers {
			a.togglePassword()
			return nil
		}
	}

	// Показ пароля доступен с любого фокуса на F2 (кроме формы/поиска
	// это не мешает: звёздочка — служебная клавиша экрана).
	if key == tcell.KeyRune && ev.Rune() == '*' && a.pageName() == pageSubscribers {
		a.togglePassword()
		return nil
	}

	// В поле поиска печатаем текст; Esc сбрасывает фильтр, Enter уводит
	// фокус обратно в список.
	if a.searchFocused() {
		switch key {
		case tcell.KeyEscape:
			a.clearSearchAndFocus()
			return nil
		case tcell.KeyEnter:
			if a.pageName() == pageBlocks {
				a.startNFTFind()
				return nil
			}
			a.focusContent()
			return nil
		}
		return ev
	}

	switch key {
	case tcell.KeyEscape:
		a.confirmQuit()
		return nil
	case tcell.KeyLeft, tcell.KeyRight:
		// На кнопках стрелки тоже переключают фокус; в таблицах и
		// прокручиваемых TextView они остаются навигацией по содержимому.
		if _, ok := a.app.GetFocus().(*tview.Button); ok {
			if key == tcell.KeyLeft {
				a.cycleFocus(-1)
			} else {
				a.cycleFocus(1)
			}
			return nil
		}
	case tcell.KeyInsert:
		if a.pageName() == pageSubscribers {
			a.openCreateForm()
			return nil
		}
	case tcell.KeyEnter:
		if a.pageName() == pageSessions && a.app.GetFocus() == a.sess.table {
			a.openSessionMenu()
			return nil
		}
	case tcell.KeyDelete:
		switch a.pageName() {
		case pageSubscribers:
			if a.app.GetFocus() == a.subs.table {
				a.confirmDelete()
				return nil
			}
		case pageBlocks:
			if a.app.GetFocus() == a.nftP.elems {
				a.confirmDeleteElement()
				return nil
			}
		case pageSessions:
			if a.app.GetFocus() == a.sess.table {
				a.confirmTerminateSession()
				return nil
			}
		}
	case tcell.KeyRune:
		switch a.pageName() {
		case pageSubscribers:
			switch ev.Rune() {
			case '/':
				a.focusSearch()
				return nil
			case 'n':
				a.findNext(1)
				return nil
			case 'N':
				a.findNext(-1)
				return nil
			}
		case pageBlocks:
			switch ev.Rune() {
			case '/':
				a.focusSearch()
				return nil
			case 'n':
				a.findNext(1)
				return nil
			case 'N':
				a.findNext(-1)
				return nil
			}
		case pageLogs:
			if ev.Rune() == '/' {
				a.focusSearch()
				return nil
			}
		case pageSessions:
			switch ev.Rune() {
			case '/':
				a.focusSearch()
				return nil
			case 'b', 'B':
				a.runBmon()
				return nil
			}
		case pageDiag:
			switch strings.ToUpper(string(ev.Rune())) {
			case "R":
				a.confirmReload()
				return nil
			case "S":
				a.confirmRestart()
				return nil
			case "K":
				a.confirmHardRestart()
				return nil
			}
		}
	}
	return ev
}

func (a *App) searchFocused() bool {
	focus := a.app.GetFocus()
	switch a.pageName() {
	case pageLogs:
		return focus == a.logs.search
	case pageSessions:
		return focus == a.sess.search
	case pageSubscribers:
		return focus == a.subs.search
	case pageBlocks:
		return focus == a.nftP.search
	}
	return false
}

func (a *App) focusSearch() {
	switch a.pageName() {
	case pageLogs:
		a.app.SetFocus(a.logs.search)
	case pageSessions:
		a.app.SetFocus(a.sess.search)
	case pageSubscribers:
		a.app.SetFocus(a.subs.search)
	case pageBlocks:
		a.app.SetFocus(a.nftP.search)
	}
}

func (a *App) focusContent() {
	switch a.pageName() {
	case pageLogs:
		a.app.SetFocus(a.logs.view)
	case pageSessions:
		a.app.SetFocus(a.sess.table)
	case pageSubscribers:
		a.app.SetFocus(a.subs.table)
	case pageBlocks:
		a.app.SetFocus(a.nftP.elems)
	}
}

func (a *App) clearSearchAndFocus() {
	switch a.pageName() {
	case pageLogs:
		a.logs.ClearSearch()
		a.app.SetFocus(a.logs.view)
	case pageSessions:
		a.sess.ClearSearch()
		a.app.SetFocus(a.sess.table)
	case pageSubscribers:
		a.subs.ClearSearch()
		a.app.SetFocus(a.subs.table)
	case pageBlocks:
		a.nftP.ClearSearch()
		a.app.SetFocus(a.nftP.sets)
	}
}

// findNext — Find Next/Previous для абонентов и IP в nft set-ах.
func (a *App) findNext(delta int) {
	switch a.pageName() {
	case pageSubscribers:
		login, idx, total := a.subs.FindNext(delta)
		if total == 0 {
			a.setStatus("warn", "совпадений нет")
			return
		}
		a.setStatusf("info", "абонент %s (%d из %d)", login, idx, total)
	case pageBlocks:
		a.stepNFTFind(delta)
	}
}

func (a *App) startNFTFind() {
	q := a.nftP.SearchQuery()
	if q == "" {
		a.setStatus("warn", "введите IP для поиска")
		return
	}
	if q == a.nftMatchQuery && len(a.nftMatches) > 0 {
		a.applyNFTMatch(0)
		return
	}
	if !a.busyFind.CompareAndSwap(false, true) {
		return
	}
	a.setStatus("info", "поиск IP "+q+"…")
	go func() {
		defer a.busyFind.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()
		matches, err := a.svc.FindIP(ctx, q)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "поиск IP: "+err.Error())
				return
			}
			a.nftMatchQuery = q
			a.nftMatches = matches
			a.nftMatchIdx = 0
			if len(matches) == 0 {
				a.setStatus("warn", "IP "+q+" не найден ни в одном set")
				return
			}
			a.applyNFTMatch(0)
		})
	}()
}

func (a *App) stepNFTFind(delta int) {
	q := a.nftP.SearchQuery()
	if q == "" {
		a.setStatus("warn", "введите IP для поиска")
		return
	}
	if q != a.nftMatchQuery || len(a.nftMatches) == 0 {
		a.startNFTFind()
		return
	}
	idx := (a.nftMatchIdx + delta + len(a.nftMatches)) % len(a.nftMatches)
	a.applyNFTMatch(idx)
}

func (a *App) applyNFTMatch(i int) {
	if i < 0 || i >= len(a.nftMatches) {
		return
	}
	m := a.nftMatches[i]
	a.nftMatchIdx = i
	a.nftP.SetPendingIP(m.IP)
	if !a.nftP.SelectSet(m.Set) {
		a.setStatus("err", "set "+m.Set+" исчез из nft")
		return
	}
	a.loadElements(m.Set)
	a.app.SetFocus(a.nftP.elems)
	a.setStatusf("info", "IP %s в set %s (%d из %d)", m.IP, m.Set, i+1, len(a.nftMatches))
}

func (a *App) pageName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.current
}

func (a *App) showPage(name string, load bool) {
	a.mu.Lock()
	a.current = name
	a.mu.Unlock()

	a.pages.SwitchToPage(name)
	if ring := a.rings[name]; ring != nil && len(ring.items) > 0 {
		ring.index = 0
		a.app.SetFocus(ring.items[0])
	} else {
		a.app.SetFocus(nil)
	}
	a.help.SetText(helpText(name))
	if load {
		a.setStatus("info", pageHint(name))
		a.loadCurrent()
	}
}

func (a *App) cycleFocus(delta int) {
	ring := a.rings[a.pageName()]
	if ring == nil || len(ring.items) < 2 {
		return
	}
	cur := a.app.GetFocus()
	idx := ring.indexOf(cur)
	if idx < 0 {
		idx = 0
	}
	next := (idx + delta + len(ring.items)) % len(ring.items)
	ring.index = next
	a.app.SetFocus(ring.items[next])
}

func (a *App) loadCurrent() {
	switch a.pageName() {
	case pageDashboard:
		a.loadDashboard()
	case pageSubscribers:
		a.loadSubs("")
	case pageBlocks:
		a.loadSets()
	case pageLogs:
		a.loadLogs()
	case pageSessions:
		a.loadSessions()
	case pageDiag:
		a.loadDiag()
	}
}

// contextWithTimeout — контекст относительно времени жизни приложения.
func contextWithTimeout(a *App, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, d)
}

// queue безопасно обновляет UI из фоновой горутины.
func (a *App) queue(f func()) {
	if a.ctx.Err() != nil {
		return
	}
	a.app.QueueUpdateDraw(f)
}

func (a *App) setStatus(level, msg string) {
	a.status.SetText(colorTag(level) + time.Now().Format("15:04:05") + "[-] " + tview.Escape(msg))
}

func (a *App) setStatusf(level, format string, args ...any) {
	a.setStatus(level, fmt.Sprintf(format, args...))
}

func (a *App) loadDashboard() {
	if !a.busyDash.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busyDash.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()
		d := a.svc.Dashboard(ctx)
		a.queue(func() {
			a.dash.Render(d)
			if len(d.Errors) > 0 {
				a.setStatus("err", strings.Join(d.Errors, "; "))
			}
		})
	}()
}

func (a *App) loadSubs(selectLogin string) {
	if !a.busySubs.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busySubs.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
		defer cancel()
		rows, err := a.svc.ListSubscribers(ctx)
		a.queue(func() {
			if err != nil {
				a.subs.RenderError(err)
				a.setStatus("err", "список абонентов: "+err.Error())
				return
			}
			a.subs.SetRows(rows, selectLogin)
		})
	}()
}

func (a *App) loadDetails(login string) {
	if login == "" {
		return
	}
	gen := a.detailGen.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		d, err := a.svc.Details(ctx, login)
		a.queue(func() {
			if gen != a.detailGen.Load() {
				return
			}
			if a.pageName() != pageSubscribers {
				return
			}
			a.subs.RenderDetails(d, err)
		})
	}()
}

func (a *App) loadSets() {
	if !a.busySets.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busySets.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
		defer cancel()
		sets, err := a.svc.NFTSets(ctx)
		a.queue(func() {
			if err != nil {
				a.nftP.RenderError(err)
				a.setStatus("err", "nft: "+err.Error())
				return
			}
			a.nftP.SetSets(sets, a.nftP.SelectedSet())
			if name := a.nftP.SelectedSet(); name != "" {
				a.loadElements(name)
			}
		})
	}()
}

func (a *App) loadElements(name string) {
	if name == "" {
		return
	}
	gen := a.elemGen.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
		defer cancel()
		els, err := a.svc.NFTSetElements(ctx, name)
		a.queue(func() {
			if gen != a.elemGen.Load() || a.pageName() != pageBlocks {
				return
			}
			if a.nftP.SelectedSet() != name {
				return
			}
			a.nftP.SetElements(name, els, err)
		})
	}()
}

func (a *App) loadLogs() {
	if !a.busyLogs.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busyLogs.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		lines, err := a.svc.LogTail(ctx)
		a.queue(func() {
			if a.pageName() != pageLogs {
				return
			}
			a.logs.Render(lines, err)
		})
	}()
}

func (a *App) loadSessions() {
	if !a.busySess.CompareAndSwap(false, true) {
		return
	}
	key := a.sess.SelectedKey()
	go func() {
		defer a.busySess.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		rows, err := a.svc.Sessions(ctx)
		a.queue(func() {
			if a.pageName() != pageSessions {
				return
			}
			if err != nil {
				a.sess.RenderError(err)
				a.setStatus("err", "accel-cmd show sessions: "+err.Error())
				return
			}
			a.sess.SetSessions(rows, key)
		})
	}()
}

func (a *App) loadDiag() {
	if !a.busyDiag.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busyDiag.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		checks := a.svc.Diagnostics(ctx)
		a.queue(func() {
			if a.pageName() != pageDiag {
				return
			}
			a.diag.Render(checks)
			ok, warn, fail := service.CountsByStatus(checks)
			level := "ok"
			if fail > 0 {
				level = "err"
			} else if warn > 0 {
				level = "warn"
			}
			a.setStatusf(level, "диагностика: OK %d, WARN %d, FAIL %d", ok, warn, fail)
		})
	}()
}

// --- модальные окна -------------------------------------------------------

func (a *App) openModal(p tview.Primitive, focus tview.Primitive) {
	if a.modalOpen {
		return
	}
	a.prevFocus = a.app.GetFocus()
	a.modalOpen = true
	a.pages.AddPage(pageModal, p, true, true)
	a.pages.SwitchToPage(pageModal)
	if focus != nil {
		a.app.SetFocus(focus)
	}
}

func (a *App) closeModal() {
	if !a.modalOpen {
		return
	}
	a.modalOpen = false
	a.pages.RemovePage(pageModal)
	a.pages.SwitchToPage(a.pageName())
	if a.prevFocus != nil {
		a.app.SetFocus(a.prevFocus)
	} else if ring := a.rings[a.pageName()]; ring != nil && len(ring.items) > 0 {
		a.app.SetFocus(ring.items[0])
	}
}

func (a *App) showConfirm(title, text, okLabel string, onOK func()) {
	if a.modalOpen {
		return
	}
	m := tview.NewModal().
		SetText(tview.Escape(text)).
		AddButtons([]string{okLabel, "Отмена"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.closeModal()
			if buttonIndex == 0 && onOK != nil {
				onOK()
			}
		})
	m.SetBackgroundColor(colBg)
	m.SetTextColor(colText)
	m.SetBorder(true)
	m.SetBorderColor(colBorder)
	m.SetTitle(" " + title + " ")
	m.SetTitleColor(colTitle)
	a.openModal(m, m)
}

func (a *App) showInfo(title, text string) {
	if a.modalOpen {
		return
	}
	m := tview.NewModal().
		SetText(tview.Escape(text)).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.closeModal()
		})
	m.SetBackgroundColor(colBg)
	m.SetTextColor(colText)
	m.SetBorder(true)
	m.SetBorderColor(colBorder)
	m.SetTitle(" " + title + " ")
	m.SetTitleColor(colTitle)
	a.openModal(m, m)
}

func (a *App) confirmQuit() {
	a.showConfirm("Выход", "Завершить работу accel-menu-go?", "Выход", func() {
		a.cancel()
		a.app.Stop()
	})
}

// helpText — постоянная нижняя панель F-клавиш. Держим её короткой,
// чтобы на 80 колонках были видны все обязательные клавиши; контекстные
// подсказки выводятся в статус-строку при переходе на экран.
func helpText(page string) string {
	_ = page
	return "[yellow]F1[white] Дашборд  [yellow]F2[white] Абоненты  [yellow]F3[white] Блокировки  " +
		"[yellow]F4[white] Логи  [yellow]F6[white] Сессии  [yellow]F9[white] Диагностика  " +
		"[yellow]F10/ESC[white] Выход"
}

func pageHint(page string) string {
	switch page {
	case pageSubscribers:
		return "/ — поиск (логин/MAC/IP), n/N — след/пред, * / F7 — пароль, Ins/Del"
	case pageBlocks:
		return "/ — поиск IP, n/N — след/пред, F8 — очистить set, Del — удалить IP"
	case pageLogs:
		return "/ или Ctrl+F — поиск, Esc — сброс, PgUp/PgDn — прокрутка"
	case pageSessions:
		return "Enter — меню (bmon/прервать), B — bmon, Del — прервать, / — поиск"
	case pageDiag:
		return "R — reload, S — restart, K — hard restart, F5 — проверки"
	default:
		return "обновление каждые 2 с; F5 — обновить"
	}
}
