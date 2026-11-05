package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"accel-menu/internal/service"
)

type dashboardPage struct {
	root *tview.Grid
	sys  *tview.TextView
	net  *tview.TextView
	svc  *tview.TextView
}

func newDashboardPage() *dashboardPage {
	sys := newPanel("СИСТЕМА")
	net := newPanel("СЕТЬ")
	svc := newPanel("СЕРВИСЫ / СЕССИИ")

	grid := tview.NewGrid().SetRows(9, 0).SetColumns(0, 0)
	grid.AddItem(sys, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(net, 0, 1, 1, 1, 0, 0, false)
	grid.AddItem(svc, 1, 0, 1, 2, 0, 0, false)
	grid.SetBackgroundColor(colBg)

	return &dashboardPage{root: grid, sys: sys, net: net, svc: svc}
}

func (p *dashboardPage) Render(d service.Dashboard) {
	p.sys.SetTitle(fmt.Sprintf(" СИСТЕМА — %s ", d.Now.Format("15:04:05")))
	p.net.SetTitle(fmt.Sprintf(" СЕТЬ — %s ", d.Iface))
	p.svc.SetTitle(" СЕРВИСЫ / СЕССИИ ")

	var b strings.Builder
	load := fmt.Sprintf("%.2f / %.2f / %.2f", d.Load.One, d.Load.Five, d.Load.Fifteen)
	fmt.Fprintf(&b, "[yellow]Load[-]    %s\n", load)
	fmt.Fprintf(&b, "[yellow]RAM[-]     %d / %d MB (%.0f%%)\n", d.MemUsedMB, d.MemTotalMB, d.Mem.UsedPercent)
	fmt.Fprintf(&b, "[yellow]CPU[-]     %s\n", orDash(d.CPU))
	fmt.Fprintf(&b, "[yellow]RSS[-]     %s\n", formatBytes(float64(d.RSSKB)*1024))
	fmt.Fprintf(&b, "[yellow]Uptime[-]  %s\n", orDash(d.Uptime))
	p.sys.SetText(b.String())

	b.Reset()
	fmt.Fprintf(&b, "[yellow]Iface[-]     %s\n", d.Iface)
	fmt.Fprintf(&b, "[yellow]Rx[-]        [white]%s[-]\n", formatRate(d.RxRate))
	fmt.Fprintf(&b, "[yellow]Tx[-]        [white]%s[-]\n", formatRate(d.TxRate))
	fmt.Fprintf(&b, "[yellow]Пакеты[-]    %s / %s\n", formatCount(d.RxPackets), formatCount(d.TxPackets))
	ct := d.ConntrackTotal
	if ct == 0 {
		ct = d.Conntrack
	}
	suffix := ""
	if d.Conntrack > 0 {
		suffix = fmt.Sprintf(" (абон. %d)", d.Conntrack)
	}
	fmt.Fprintf(&b, "[yellow]Conntrack[-] %d / %d%s\n", ct, d.ConntrackMax, suffix)
	if len(d.MasqIfaces) > 0 {
		fmt.Fprintf(&b, "[yellow]Masq[-]      %s\n", strings.Join(d.MasqIfaces, ", "))
	} else {
		b.WriteString("[yellow]Masq[-]      [red]нет правил[-]\n")
	}
	if len(d.MasqIPs) > 0 {
		fmt.Fprintf(&b, "[yellow]Внешние IP[-] %s\n", strings.Join(d.MasqIPs, ", "))
	}
	fmt.Fprintf(&b, "[yellow]ip_forward[-] %s\n", yesNo(d.IPForward))
	p.net.SetText(b.String())

	b.Reset()
	fmt.Fprintf(&b, "[yellow]accel-ppp[-]   %s / %s\n", stateTag(d.AccelState), orDash(d.AccelEnabled))
	fmt.Fprintf(&b, "[yellow]Uptime[-]      %s\n", orDash(d.AccelUptime))
	fmt.Fprintf(&b, "[yellow]Сессии[-]      %d active, %d starting\n", d.SessionsActive, d.SessionsStarting)
	fmt.Fprintf(&b, "[yellow]PPPoE / IPoE[-] %d / %d\n", d.PPPoE, d.IPoE)
	b.WriteString("[yellow]Модули[-]      ")
	if len(d.Modules) == 0 {
		b.WriteString("[red]n/a[-]\n")
	} else {
		for i, m := range d.Modules {
			if i > 0 {
				b.WriteString("  ")
			}
			name := strings.ToUpper(m.Name)
			if m.Present {
				fmt.Fprintf(&b, "[white]%s %d[-]", name, m.Active)
			} else {
				fmt.Fprintf(&b, "[gray]%s —[-]", name)
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[yellow]RADIUS[-]      %s, fail %d\n", stateTag(d.RadiusState), d.RadiusFail)
	fmt.Fprintf(&b, "[yellow]FreeRADIUS[-]  %s\n", stateTag(d.RadiusCTR))
	fmt.Fprintf(&b, "[yellow]MariaDB[-]     %s\n", stateTag(d.DBCTR))
	dbOK := "[red]нет[-]"
	if d.DBOK {
		dbOK = "[white]OK[-]"
	}
	fmt.Fprintf(&b, "[yellow]БД[-]          %s\n", dbOK)
	b.WriteString("\n")
	if len(d.Errors) > 0 {
		for _, e := range d.Errors {
			fmt.Fprintf(&b, "[red]! %s[-]\n", tview.Escape(e))
		}
	}
	if len(d.Warnings) > 0 {
		for _, w := range d.Warnings {
			fmt.Fprintf(&b, "[yellow]! %s[-]\n", tview.Escape(w))
		}
	}
	if len(d.Errors) == 0 && len(d.Warnings) == 0 {
		b.WriteString("[white]все компоненты в норме[-]\n")
	}
	p.svc.SetText(b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func yesNo(v bool) string {
	if v {
		return "[white]включён[-]"
	}
	return "[red]выключен[-]"
}

func stateTag(state string) string {
	s := strings.TrimSpace(state)
	low := strings.ToLower(s)
	switch {
	case s == "":
		return "[red]n/a[-]"
	case strings.HasPrefix(low, "active"), strings.HasPrefix(low, "running"), strings.HasPrefix(low, "healthy"),
		strings.HasPrefix(low, "enabled"):
		return "[white]" + tview.Escape(s) + "[-]"
	case strings.HasPrefix(low, "inactive"), strings.HasPrefix(low, "failed"), strings.HasPrefix(low, "exited"),
		strings.HasPrefix(low, "dead"), strings.HasPrefix(low, "нет"), strings.HasPrefix(low, "not-found"):
		return "[red]" + tview.Escape(s) + "[-]"
	default:
		return "[yellow]" + tview.Escape(s) + "[-]"
	}
}
