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
	fmt.Fprintf(&b, "[yellow]Iface[-]   %s  [yellow]Rx[-] %s  [yellow]Tx[-] %s\n",
		d.Iface, formatRate(d.RxRate), formatRate(d.TxRate))
	ct := d.ConntrackTotal
	if ct == 0 {
		ct = d.Conntrack
	}
	ctSuffix := ""
	if d.Conntrack > 0 {
		ctSuffix = fmt.Sprintf(" (абон. %d)", d.Conntrack)
	}
	fmt.Fprintf(&b, "[yellow]Пакеты[-]  %s / %s  [yellow]CT[-] %d/%d%s\n",
		formatCount(d.RxPackets), formatCount(d.TxPackets), ct, d.ConntrackMax, ctSuffix)

	up := d.Uplink
	if up.Iface == "" {
		b.WriteString("[yellow]Uplink[-]  [red]не определён[-]\n")
	} else {
		state := up.OperState
		if state == "" {
			state = "—"
		}
		stateTag := "[white]" + tview.Escape(state) + "[-]"
		if !strings.EqualFold(state, "up") {
			stateTag = "[red]" + tview.Escape(state) + "[-]"
		}
		duplex := up.Duplex
		if duplex == "" {
			duplex = "—"
		}
		speed := "—"
		if up.SpeedMbps > 0 {
			speed = fmt.Sprintf("%d Mb/s", up.SpeedMbps)
		}
		fmt.Fprintf(&b, "[yellow]Uplink[-]  %s %s/%s %s\n",
			tview.Escape(up.Iface), stateTag, tview.Escape(duplex), speed)
		fmt.Fprintf(&b, "[yellow]Up Rx[-]   %s  [yellow]Tx[-] %s\n",
			formatRate(up.RxRate), formatRate(up.TxRate))
		fmt.Fprintf(&b, "[yellow]Up пакеты[-] %s / %s  [yellow]всего[-] %s / %s\n",
			formatCount(up.RxPackets), formatCount(up.TxPackets),
			formatBytes(float64(up.RxBytes)), formatBytes(float64(up.TxBytes)))
		fmt.Fprintf(&b, "[yellow]Up дропы[-] %d / %d  [yellow]ошибки[-] %d / %d\n",
			up.RxDropped, up.TxDropped, up.RxErrors, up.TxErrors)
	}

	if len(d.MasqIfaces) > 0 {
		fmt.Fprintf(&b, "[yellow]Masq[-]     %s", strings.Join(d.MasqIfaces, ", "))
	} else {
		b.WriteString("[yellow]Masq[-]     [red]нет правил[-]")
	}
	if len(d.MasqIPs) > 0 {
		fmt.Fprintf(&b, "  [yellow]IP[-] %s", strings.Join(d.MasqIPs, ", "))
	}
	fmt.Fprintf(&b, "  [yellow]fwd[-] %s\n", yesNo(d.IPForward))
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
