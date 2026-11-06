package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"accel-menu/internal/procfs"
)

// ConntrackEntry — запись таблицы conntrack для выбранного абонента.
type ConntrackEntry = procfs.ConntrackEntry

// Conntrack возвращает conntrack-записи, в которых участвует IP абонента.
func (s *Service) Conntrack(ctx context.Context, ip string) ([]ConntrackEntry, error) {
	ip = strings.TrimSpace(ip)
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("некорректный IP сессии: %q", ip)
	}
	return procfs.ReadConntrackForIP(ip)
}

// AccelConfig читает конфигурацию accel-ppp для просмотра в TUI.
func (s *Service) AccelConfig(ctx context.Context) ([]string, error) {
	data, err := os.ReadFile(s.cfg.AccelConf)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.cfg.AccelConf, err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// AccelVersion возвращает версию accel-ppp.
func (s *Service) AccelVersion(ctx context.Context) (string, error) {
	return s.accel.ShowVersion(ctx)
}

// KernelVersion читает версию ядра.
func (s *Service) KernelVersion(ctx context.Context) (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "", fmt.Errorf("пустой /proc/sys/kernel/osrelease")
	}
	return v, nil
}

// VLANCount считает VLAN-интерфейсы, созданные vlan_mon.
func (s *Service) VLANCount(ctx context.Context) (int, error) {
	out, err := s.run.RunSudo(ctx, "ip", "-d", "-o", "link", "show", "type", "vlan")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}
