package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"accel-menu/internal/accel"
	"accel-menu/internal/execx"
	"accel-menu/internal/service"
	"accel-menu/internal/store"
)

const (
	labelLogin = "Логин"
	labelType  = "Тип авторизации"
	labelPass  = "Пароль"
	labelIP    = "IP (пусто — из пула)"
	labelDown  = "Скорость вниз, кбит/с"
	labelUp    = "Скорость вверх, кбит/с"
	labelGroup = "Группа"
)

func (a *App) openCreateForm() {
	if a.modalOpen {
		return
	}
	form := tview.NewForm()
	form.SetBackgroundColor(colBg)
	form.SetItemPadding(0)
	form.SetBorder(true)
	form.SetBorderColor(colBorder)
	form.SetTitle(" НОВЫЙ АБОНЕНТ ")
	form.SetTitleColor(colTitle)

	form.AddInputField(labelLogin, "", 32, nil, nil)
	form.AddDropDown(labelType, []string{"PPPoE", "IPoE"}, 0, nil)
	form.AddInputField(labelPass, "", 32, nil, nil)
	form.AddInputField(labelIP, "", 32, nil, nil)
	form.AddInputField(labelDown, "50000", 16, nil, nil)
	form.AddInputField(labelUp, "20000", 16, nil, nil)
	form.AddInputField(labelGroup, "default", 16, nil, nil)
	form.AddTextView("", "[gray]Тип можно выбрать вручную. IPoE требует MAC-логин; для PPPoE статический IP игнорируется.[-]", 0, 1, true, false)
	form.AddButton("Сохранить", func() { a.saveNewSubscriber(form) })
	form.AddButton("Отмена", func() { a.closeModal() })
	form.SetCancelFunc(func() { a.closeModal() })

	login := formInput(form, labelLogin)
	pass := formInput(form, labelPass)
	typ := formDropDown(form, labelType)
	for _, label := range []string{labelLogin, labelPass, labelIP, labelDown, labelUp, labelGroup} {
		if in := formInput(form, label); in != nil {
			in.SetLabelWidth(28)
			in.SetFieldWidth(32)
			in.SetFieldBackgroundColor(colBg)
			in.SetFieldTextColor(colText)
		}
	}
	if typ != nil {
		typ.SetLabelWidth(28)
		typ.SetFieldWidth(16)
		typ.SetFieldBackgroundColor(colBg)
		typ.SetFieldTextColor(colText)
	}
	if pass != nil {
		pass.SetMaskCharacter('*')
	}

	manualType := false
	updatingType := false
	if typ != nil {
		typ.SetSelectedFunc(func(option string, index int) {
			if !updatingType {
				manualType = true
			}
		})
	}
	if login != nil && pass != nil {
		auto := false
		login.SetChangedFunc(func(text string) {
			// ВАЖНО: не вызываем pass.SetDisabled() на каждый символ.
			// InputField.SetDisabled() дергает FormItem.finished(-1), и Form
			// повторяет последнее действие (Tab), перескакивая фокус.
			isMAC := store.IsMAC(text)
			if typ != nil && !manualType {
				idx := 0
				if isMAC {
					idx = 1
				}
				updatingType = true
				typ.SetCurrentOption(idx)
				updatingType = false
			}
			if isMAC {
				if auto || pass.GetText() == "" {
					pass.SetText(strings.ReplaceAll(text, ":", ""))
					auto = true
				}
				return
			}
			if auto {
				pass.SetText("")
				auto = false
			}
		})
	}

	a.openModal(centered(form, 76, 20), form)
	form.SetFocus(0)
}

func formInput(form *tview.Form, label string) *tview.InputField {
	item := form.GetFormItemByLabel(label)
	if item == nil {
		return nil
	}
	in, _ := item.(*tview.InputField)
	return in
}

func formDropDown(form *tview.Form, label string) *tview.DropDown {
	item := form.GetFormItemByLabel(label)
	if item == nil {
		return nil
	}
	dd, _ := item.(*tview.DropDown)
	return dd
}

func formText(form *tview.Form, label string) string {
	if in := formInput(form, label); in != nil {
		return strings.TrimSpace(in.GetText())
	}
	return ""
}

func (a *App) saveNewSubscriber(form *tview.Form) {
	isIPoE := false
	if dd := formDropDown(form, labelType); dd != nil {
		idx, _ := dd.GetCurrentOption()
		isIPoE = idx == 1
	}
	in := service.NewSubscriberInput{
		Username: formText(form, labelLogin),
		Password: formText(form, labelPass),
		IP:       formText(form, labelIP),
		Down:     formText(form, labelDown),
		Up:       formText(form, labelUp),
		Group:    formText(form, labelGroup),
		IsIPoE:   isIPoE,
	}
	a.setStatus("info", "создание абонента "+in.Username+"…")
	go func() {
		ctx, cancel := contextWithTimeout(a, 10*time.Second)
		defer cancel()
		err := a.svc.CreateSubscriber(ctx, in)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "создание: "+err.Error())
				return
			}
			a.closeModal()
			a.setStatus("ok", "абонент "+in.Username+" создан")
			a.loadSubs(in.Username)
		})
	}()
}

// togglePassword — единая точка показа/скрытия пароля с сообщением.
func (a *App) togglePassword() {
	a.subs.TogglePassword()
	if a.subs.PasswordRevealed() {
		a.setStatus("info", "пароль показан; * или F7 — скрыть")
	} else if a.subs.SelectedLogin() != "" {
		a.setStatus("info", "пароль скрыт; * или F7 — показать")
	}
}

func (a *App) confirmDelete() {
	login := a.subs.SelectedLogin()
	if login == "" {
		a.setStatus("warn", "абонент не выбран")
		return
	}
	go func() {
		ctx, cancel := contextWithTimeout(a, 5*time.Second)
		defer cancel()
		counts, err := a.svc.Counts(ctx, login)
		a.queue(func() {
			if err != nil {
				a.showInfo("Ошибка", "Не удалось получить данные абонента:\n"+err.Error())
				return
			}
			text := fmt.Sprintf(
				"Удалить абонента %s?\n\nradcheck:     %d\nradreply:     %d\nradusergroup: %d\nradacct:      %d (история сохранится)\n\nАктивная сессия будет завершена (accel-cmd terminate username … soft).",
				login, counts.Check, counts.Reply, counts.Group, counts.Acct)
			a.showConfirm("Удаление", text, "Удалить", func() { a.doDelete(login) })
		})
	}()
}

func (a *App) doDelete(login string) {
	go func() {
		ctx, cancel := contextWithTimeout(a, 15*time.Second)
		defer cancel()
		res, err := a.svc.DeleteSubscriber(ctx, login)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "удаление: "+err.Error())
				a.showInfo("Ошибка удаления", err.Error())
				return
			}
			if res.TerminateWarning != "" {
				a.setStatus("warn", "удалён "+login+"; сессия: "+res.TerminateWarning)
			} else {
				a.setStatus("ok", "абонент "+login+" удалён")
			}
			a.loadSubs("")
		})
	}()
}

func (a *App) confirmFlushSet() {
	info, ok := a.nftP.SelectedSetInfo()
	if !ok {
		a.setStatus("warn", "set не выбран")
		return
	}
	text := fmt.Sprintf("Очистить set %s?\n\nБудет удалено элементов: %d.\nДействие необратимо.", info.Name, info.Count)
	a.showConfirm("Очистка set", text, "Очистить", func() { a.runFlush(info.Name) })
}

func (a *App) runFlush(name string) {
	go func() {
		ctx, cancel := contextWithTimeout(a, 10*time.Second)
		defer cancel()
		err := a.svc.FlushSet(ctx, name)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "flush "+name+": "+err.Error())
				a.showInfo("Ошибка nft", err.Error())
				return
			}
			a.setStatus("ok", "set "+name+" очищен")
			a.loadSets()
		})
	}()
}

func (a *App) confirmDeleteElement() {
	name := a.nftP.SelectedSet()
	ip := a.nftP.SelectedElement()
	if name == "" || ip == "" {
		a.setStatus("warn", "IP в set-е не выбран")
		return
	}
	text := fmt.Sprintf("Удалить %s из set-а %s?", ip, name)
	a.showConfirm("Удаление IP", text, "Удалить", func() { a.runDeleteElement(name, ip) })
}

func (a *App) runDeleteElement(name, ip string) {
	go func() {
		ctx, cancel := contextWithTimeout(a, 10*time.Second)
		defer cancel()
		err := a.svc.DeleteElement(ctx, name, ip)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "delete "+ip+": "+err.Error())
				a.showInfo("Ошибка nft", err.Error())
				return
			}
			a.setStatus("ok", ip+" удалён из "+name)
			a.loadSets()
		})
	}()
}

// --- сессии ---------------------------------------------------------------

// openSessionMenu — контекстное меню действий над выбранной сессией.
func (a *App) openSessionMenu() {
	ses, ok := a.sess.SelectedSession()
	if !ok {
		a.setStatus("warn", "сессия не выбрана")
		return
	}
	text := fmt.Sprintf(
		"Сессия\n\nЛогин:      %s\nIP:         %s\nИнтерфейс:  %s\nТип:        %s\n\nВыберите действие:",
		ses.Username, ses.IP, ses.IfName, ses.Type)
	m := tview.NewModal().
		SetText(tview.Escape(text)).
		AddButtons([]string{"bmon", "tcpdump", "conntrack", "Прервать", "Отмена"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.closeModal()
			switch buttonIndex {
			case 0:
				a.runBmonSession(ses)
			case 1:
				a.runTcpdumpSession(ses)
			case 2:
				a.openConntrackSession(ses)
			case 3:
				a.confirmTerminateSessionFor(ses)
			}
		})
	m.SetBackgroundColor(colBg)
	m.SetTextColor(colText)
	m.SetBorder(true)
	m.SetBorderColor(colBorder)
	m.SetTitle(" Действия с сессией ")
	m.SetTitleColor(colTitle)
	a.openModal(m, m)
}

func (a *App) runBmon() {
	ses, ok := a.sess.SelectedSession()
	if !ok {
		a.setStatus("warn", "сессия не выбрана")
		return
	}
	a.runBmonSession(ses)
}

func (a *App) runBmonSession(ses accel.Session) {
	ifname := strings.TrimSpace(ses.IfName)
	if ifname == "" {
		a.setStatus("warn", "у сессии нет имени интерфейса")
		return
	}
	bin, err := execx.Resolve("bmon")
	if err != nil {
		a.showInfo("bmon", "bmon не найден в системе: "+err.Error())
		return
	}

	var runErr error
	suspended := a.app.Suspend(func() {
		cmd := exec.Command(bin, "-p", ifname)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		runErr = cmd.Run()
	})
	if !suspended {
		a.setStatus("warn", "не удалось приостановить TUI для bmon")
		return
	}
	if runErr != nil {
		a.setStatus("err", "bmon "+ifname+": "+runErr.Error())
		a.showInfo("bmon", "bmon завершился с ошибкой:\n"+runErr.Error())
		return
	}
	a.setStatus("ok", "bmon завершён: "+ifname)
	a.loadSessions()
}

func (a *App) runTcpdump() {
	ses, ok := a.sess.SelectedSession()
	if !ok {
		a.setStatus("warn", "сессия не выбрана")
		return
	}
	a.runTcpdumpSession(ses)
}

func (a *App) runTcpdumpSession(ses accel.Session) {
	if a.modalOpen || a.popupOpen {
		return
	}
	ifname := strings.TrimSpace(ses.IfName)
	if ifname == "" {
		a.setStatus("warn", "у сессии нет имени интерфейса")
		return
	}
	bin, err := execx.Resolve("tcpdump")
	if err != nil {
		a.showInfo("tcpdump", "tcpdump не найден в системе: "+err.Error())
		return
	}
	args := []string{"-n", "-l", "-i", ifname}
	if ip := strings.TrimSpace(ses.IP); ip != "" {
		args = append(args, "host", ip)
	}

	a.tcpdump.Reset(ses)
	a.openPopup(pageTcpdump, "tcpdump", a.tcpdump.root, a.tcpdump.view)
	ctx, cancel := context.WithCancel(a.ctx)
	a.tdCancel = cancel

	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		a.tdCancel = nil
		a.closePopup()
		a.showInfo("tcpdump", err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		a.tdCancel = nil
		a.closePopup()
		a.showInfo("tcpdump", err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		cancel()
		a.tdCancel = nil
		a.closePopup()
		a.showInfo("tcpdump", "не удалось запустить tcpdump:\n"+err.Error())
		return
	}
	a.setStatus("info", "tcpdump "+ifname+" — Esc остановить")

	go func() {
		var wg sync.WaitGroup
		scan := func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				line := sc.Text()
				a.queue(func() {
					if a.popupOpen && a.popupKind == "tcpdump" {
						a.tcpdump.AddLine(line)
					}
				})
			}
		}
		wg.Add(2)
		go scan(stdout)
		go scan(stderr)
		wg.Wait()
		runErr := cmd.Wait()
		a.queue(func() {
			if a.popupOpen && a.popupKind == "tcpdump" {
				if runErr != nil && !strings.Contains(strings.ToLower(runErr.Error()), "killed") {
					a.tcpdump.Notice("tcpdump завершён: " + runErr.Error())
				} else {
					a.tcpdump.Notice("tcpdump остановлен")
				}
			}
			a.tdCancel = nil
		})
	}()
}

func (a *App) stopTcpdump() {
	if a.tdCancel != nil {
		a.tdCancel()
		a.tdCancel = nil
	}
}

func (a *App) confirmTerminateSession() {
	ses, ok := a.sess.SelectedSession()
	if !ok {
		a.setStatus("warn", "сессия не выбрана")
		return
	}
	a.confirmTerminateSessionFor(ses)
}

func (a *App) confirmTerminateSessionFor(ses accel.Session) {
	text := fmt.Sprintf(
		"Прервать сессию?\n\nЛогин:      %s\nIP:         %s\nИнтерфейс:  %s\nТип:        %s\n\nБудет выполнено:\naccel-cmd terminate if %s soft",
		ses.Username, ses.IP, ses.IfName, ses.Type, ses.IfName)
	a.showConfirm("Прерывание сессии", text, "Прервать", func() {
		a.runTerminateSession(ses.IfName)
	})
}

func (a *App) runTerminateSession(ifname string) {
	go func() {
		ctx, cancel := contextWithTimeout(a, 10*time.Second)
		defer cancel()
		out, err := a.svc.TerminateSession(ctx, ifname)
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "terminate "+ifname+": "+err.Error())
				a.showInfo("Ошибка accel-cmd", err.Error())
				return
			}
			msg := "сессия на " + ifname + " завершена"
			if s := strings.TrimSpace(out); s != "" {
				msg += ": " + s
			}
			a.setStatus("ok", msg)
			a.loadSessions()
		})
	}()
}

// --- управление демоном (F9) ----------------------------------------------

func (a *App) confirmReload() {
	a.showConfirm("Reload accel-ppp",
		"Перечитать конфигурацию accel-ppp?\n\nСессии не разрываются.",
		"Reload", func() { a.runAccelAction("reload") })
}

func (a *App) confirmRestart() {
	a.showConfirm("Restart accel-ppp",
		"Перезапустить accel-ppp?\n\nВНИМАНИЕ: все активные сессии будут завершены.",
		"Restart", func() { a.runAccelAction("restart") })
}

func (a *App) confirmHardRestart() {
	a.showConfirm("Hard restart accel-ppp",
		"Отправить SIGKILL и запустить accel-ppp заново?\n\nВНИМАНИЕ: все сессии будут сброшены.",
		"Hard restart", func() { a.runAccelAction("hard") })
}

func (a *App) runAccelAction(kind string) {
	label := kind
	switch kind {
	case "reload":
		label = "reload"
	case "restart":
		label = "restart"
	case "hard":
		label = "hard restart"
	}
	a.setStatus("info", "accel-ppp: "+label+"…")
	go func() {
		ctx, cancel := contextWithTimeout(a, 45*time.Second)
		defer cancel()
		var err error
		switch kind {
		case "reload":
			_, err = a.svc.ReloadAccel(ctx)
		case "restart":
			_, err = a.svc.RestartAccel(ctx)
		case "hard":
			_, err = a.svc.HardRestartAccel(ctx)
		}
		a.queue(func() {
			if err != nil {
				a.setStatus("err", "accel-ppp "+label+": "+err.Error())
				a.showInfo("Ошибка управления", err.Error())
				return
			}
			a.setStatus("ok", "accel-ppp: "+label+" выполнен")
		})

		wait := 2 * time.Second
		if kind == "reload" {
			wait = 500 * time.Millisecond
		}
		select {
		case <-time.After(wait):
		case <-a.ctx.Done():
			return
		}
		a.loadDiag()
	}()
}
