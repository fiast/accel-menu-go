package tui

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"accel-menu/internal/accel"
)

func (a *App) openPopup(name, kind string, root tview.Primitive, focus tview.Primitive) {
	if a.modalOpen || a.popupOpen {
		return
	}
	a.prevFocus = a.app.GetFocus()
	a.prevPage = a.pageName()
	a.popupOpen = true
	a.popupKind = kind
	a.pages.AddPage(name, root, true, true)
	a.pages.SwitchToPage(name)
	if focus != nil {
		a.app.SetFocus(focus)
	}
}

func (a *App) closePopup() {
	if !a.popupOpen {
		return
	}
	page := pageConntrack
	switch a.popupKind {
	case "config":
		page = pageConfig
	case "tcpdump":
		page = pageTcpdump
		a.stopTcpdump()
	}
	a.popupOpen = false
	a.popupKind = ""
	a.pages.RemovePage(page)
	a.pages.SwitchToPage(a.prevPage)
	if a.prevFocus != nil {
		a.app.SetFocus(a.prevFocus)
	} else if ring := a.rings[a.prevPage]; ring != nil && len(ring.items) > 0 {
		a.app.SetFocus(ring.items[0])
	}
}

func (a *App) popupSearchFocused() bool {
	focus := a.app.GetFocus()
	switch a.popupKind {
	case "conntrack":
		return focus == a.conntrack.search
	case "config":
		return focus == a.conf.search
	}
	return false
}

func (a *App) focusPopupSearch() {
	switch a.popupKind {
	case "conntrack":
		a.app.SetFocus(a.conntrack.search)
	case "config":
		a.app.SetFocus(a.conf.search)
	}
}

func (a *App) focusPopupContent() {
	switch a.popupKind {
	case "conntrack":
		a.app.SetFocus(a.conntrack.table)
	case "config":
		a.app.SetFocus(a.conf.view)
	}
}

func (a *App) popupInput(ev *tcell.EventKey) *tcell.EventKey {
	key := ev.Key()
	switch key {
	case tcell.KeyEscape:
		a.closePopup()
		return nil
	case tcell.KeyF5:
		a.refreshPopup()
		return nil
	case tcell.KeyF1, tcell.KeyF2, tcell.KeyF3, tcell.KeyF4,
		tcell.KeyF6, tcell.KeyF9, tcell.KeyF10:
		return nil
	case tcell.KeyTab:
		if a.popupKind == "tcpdump" {
			return nil
		}
		if a.popupSearchFocused() {
			a.focusPopupContent()
		} else {
			a.focusPopupSearch()
		}
		return nil
	case tcell.KeyBacktab:
		if a.popupSearchFocused() {
			a.focusPopupContent()
		} else {
			a.focusPopupSearch()
		}
		return nil
	case tcell.KeyCtrlF:
		a.focusPopupSearch()
		return nil
	case tcell.KeyCtrlN:
		a.popupFindNext(1)
		return nil
	case tcell.KeyCtrlP:
		a.popupFindNext(-1)
		return nil
	}

	if a.popupSearchFocused() {
		if key == tcell.KeyEnter {
			if a.popupKind == "conntrack" {
				a.focusPopupContent()
			} else {
				a.focusPopupContent()
			}
			return nil
		}
		return ev
	}
	if key == tcell.KeyRune {
		switch ev.Rune() {
		case '/':
			if a.popupKind == "tcpdump" {
				return ev
			}
			a.focusPopupSearch()
			return nil
		case 'n':
			a.popupFindNext(1)
			return nil
		case 'N':
			a.popupFindNext(-1)
			return nil
		}
	}
	return ev
}

func (a *App) popupFindNext(delta int) {
	if a.popupKind != "conntrack" {
		return
	}
	key, idx, total := a.conntrack.FindNext(delta)
	if total == 0 {
		a.setStatus("warn", "совпадений нет")
		return
	}
	a.setStatusf("info", "conntrack %s (%d из %d)", key, idx, total)
}

func (a *App) refreshPopup() {
	switch a.popupKind {
	case "conntrack":
		a.loadConntrack()
	case "config":
		a.loadAccelConfig()
	}
}

// openConntrackSelected открывает conntrack для выбранной сессии.
func (a *App) openConntrackSelected() {
	ses, ok := a.sess.SelectedSession()
	if !ok {
		a.setStatus("warn", "сессия не выбрана")
		return
	}
	a.openConntrackSession(ses)
}

func (a *App) openConntrackSession(ses accel.Session) {
	if strings.TrimSpace(ses.IP) == "" {
		a.setStatus("warn", "у сессии нет IP — conntrack невозможен")
		return
	}
	a.ctSession = ses
	a.conntrack.Open(ses)
	a.openPopup(pageConntrack, "conntrack", a.conntrack.root, a.conntrack.search)
	a.setStatus("info", "conntrack "+ses.Username+" / "+ses.IP+" — / поиск, n/N, Esc закрыть")
	a.loadConntrack()
}

func (a *App) loadConntrack() {
	if !a.popupOpen || a.popupKind != "conntrack" {
		return
	}
	if !a.busyCT.CompareAndSwap(false, true) {
		return
	}
	target := a.ctSession
	go func() {
		defer a.busyCT.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		rows, err := a.svc.Conntrack(ctx, target.IP)
		a.queue(func() {
			if !a.popupOpen || a.popupKind != "conntrack" || a.ctSession.IP != target.IP {
				return
			}
			a.conntrack.Render(rows, err)
			if err != nil {
				a.setStatus("err", "conntrack: "+err.Error())
			}
		})
	}()
}

// openAccelConfig открывает accel-ppp.conf в окне с поиском и прокруткой.
func (a *App) openAccelConfig() {
	if a.modalOpen || a.popupOpen {
		return
	}
	a.conf.ClearSearch()
	a.openPopup(pageConfig, "config", a.conf.root, a.conf.search)
	a.setStatus("info", "конфиг accel-ppp — / поиск, PgUp/PgDn, Esc закрыть")
	a.loadAccelConfig()
}

func (a *App) loadAccelConfig() {
	if !a.popupOpen || a.popupKind != "config" {
		return
	}
	if !a.busyConf.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busyConf.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		lines, err := a.svc.AccelConfig(ctx)
		a.queue(func() {
			if !a.popupOpen || a.popupKind != "config" {
				return
			}
			a.conf.Render(lines, err)
			if err != nil {
				a.setStatus("err", "конфиг: "+err.Error())
			}
		})
	}()
}

// ConntrackPage используется тестами.
func (a *App) ConntrackPage() *conntrackPage { return a.conntrack }

// ConfigPage используется тестами.
func (a *App) ConfigPage() *logsPage { return a.conf }
