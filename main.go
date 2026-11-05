// accel-menu-go — TUI-замена bash-скрипту accel-menu для BRAS
// (accel-ppp + FreeRADIUS + MariaDB).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"accel-menu/internal/accel"
	"accel-menu/internal/config"
	"accel-menu/internal/docker"
	"accel-menu/internal/execx"
	"accel-menu/internal/nft"
	"accel-menu/internal/service"
	"accel-menu/internal/store"
	"accel-menu/internal/tui"
)

// version подставляется через -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		check       bool
		showVersion bool
	)
	flag.BoolVar(&check, "check", false, "выполнить диагностику без TUI и выйти")
	flag.BoolVar(&showVersion, "version", false, "показать версию")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "accel-menu-go %s — TUI мониторинга BRAS\n\n", version)
		fmt.Fprintf(os.Stderr, "Использование: %s [--check] [--version]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		fmt.Printf("accel-menu-go %s (go %s, %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфигурация: "+err.Error())
		return 2
	}
	cfg.Version = version

	run := execx.New(os.Geteuid() != 0, cfg.CmdTimeout)
	st, err := store.Open(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "БД: "+err.Error())
		return 1
	}
	defer st.Close()

	svc := service.New(cfg, st, accel.New(run, cfg.AccelCmd), nft.New(run, cfg.NFTFamily, cfg.NFTTable), docker.New(run), run)

	if check {
		return runCheck(svc, cfg)
	}
	if err := runTUI(svc, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "TUI: "+err.Error())
		return 1
	}
	return 0
}

func runCheck(svc *service.Service, cfg config.Config) int {
	fmt.Printf("BRAS self-check: %s\n", time.Now().Format("02.01.2006 15:04:05"))
	fmt.Printf("Конфиг: %s\n", cfg.ConfigPath)
	fmt.Printf("БД: %s/%s@%s, интерфейс: %s, лог: %s\n\n", cfg.DBUser, cfg.DBName, cfg.DBHost, cfg.Iface, cfg.LogPath)

	dctx, dcancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer dcancel()
	checks := svc.Diagnostics(dctx)
	for _, c := range checks {
		fmt.Printf("%-6s %-30s %s\n", c.Status.Label(), c.Name, service.TrimDetail(c.Detail))
	}
	ok, warn, fail := service.CountsByStatus(checks)
	fmt.Printf("\nИТОГО: OK %d, WARN %d, FAIL %d\n", ok, warn, fail)

	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	d := svc.Dashboard(sctx)
	fmt.Printf("\nСнимок дашборда:\n")
	fmt.Printf("  load      %.2f / %.2f / %.2f\n", d.Load.One, d.Load.Five, d.Load.Fifteen)
	fmt.Printf("  RAM       %d / %d MB (%.0f%%)\n", d.MemUsedMB, d.MemTotalMB, d.Mem.UsedPercent)
	fmt.Printf("  сессии    %d (PPPoE %d, IPoE %d), starting %d\n", d.SessionsActive, d.PPPoE, d.IPoE, d.SessionsStarting)
	fmt.Printf("  сеть      Rx %s, Tx %s, пакеты %d/%d\n",
		humanRate(d.RxRate), humanRate(d.TxRate), d.RxPackets, d.TxPackets)
	fmt.Printf("  conntrack %d / %d (абон. %d)\n", d.ConntrackTotal, d.ConntrackMax, d.Conntrack)
	fmt.Printf("  docker    %s: %s, %s: %s\n", cfg.RadiusCTR, d.RadiusCTR, cfg.DBCTR, d.DBCTR)
	fmt.Printf("  accel     %s / %s, uptime %s\n", d.AccelState, d.AccelEnabled, d.Uptime)
	if len(d.Modules) > 0 {
		fmt.Printf("  модули   ")
		for i, m := range d.Modules {
			if i > 0 {
				fmt.Printf("  ")
			}
			if m.Present {
				fmt.Printf("%s=%d", m.Name, m.Active)
			} else {
				fmt.Printf("%s=off", m.Name)
			}
		}
		fmt.Println()
	}
	for _, e := range d.Errors {
		fmt.Printf("  [ошибка] %s\n", e)
	}
	for _, w := range d.Warnings {
		fmt.Printf("  [warn]   %s\n", w)
	}

	if fail > 0 {
		return 1
	}
	return 0
}

func humanRate(v float64) string {
	switch {
	case v >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", v/(1024*1024))
	case v >= 1024:
		return fmt.Sprintf("%.1f KB/s", v/1024)
	default:
		return fmt.Sprintf("%.0f B/s", v)
	}
}

func runTUI(svc *service.Service, cfg config.Config) error {
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		// tcell не умеет TERM=dumb; при реальном TTY это безопасный дефолт.
		_ = os.Setenv("TERM", "xterm-256color")
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("нужен терминал: запустите в интерактивной консоли или через ssh -t")
	}
	return tui.New(svc, cfg).Run()
}
