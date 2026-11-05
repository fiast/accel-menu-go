package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// formatBytes — человекочитаемый размер: 1536 -> "1.5 KB".
func formatBytes(v float64) string {
	if v < 0 {
		v = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// formatRate — скорость в байтах/с.
func formatRate(v float64) string { return formatBytes(v) + "/s" }

// formatCount — большие числа: 1200000 -> "1.2M".
func formatCount(v uint64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fG", float64(v)/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fk", float64(v)/1_000)
	default:
		return fmt.Sprintf("%d", v)
	}
}

// maskValue скрывает значение, если атрибут похож на пароль.
func maskValue(attr, value string) string {
	low := strings.ToLower(attr)
	if strings.Contains(low, "password") || strings.Contains(low, "secret") {
		if value == "" {
			return "(пусто)"
		}
		return "********"
	}
	return value
}

// formatTS форматирует дату/время для карточки.
func formatTS(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("02.01 15:04")
}

// formatDuration переводит секунды в компактный вид.
func formatDuration(sec int64) string {
	if sec <= 0 {
		return "0 с"
	}
	d := sec / 86400
	sec %= 86400
	h := sec / 3600
	sec %= 3600
	m := sec / 60
	s := sec % 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dд %dч", d, h)
	case h > 0:
		return fmt.Sprintf("%dч %dм", h, m)
	case m > 0:
		return fmt.Sprintf("%dм %dс", m, s)
	default:
		return fmt.Sprintf("%dс", s)
	}
}

// colorizeLogLine подсвечивает важные строки лога.
func colorizeLogLine(line string) string {
	base := logBaseColor(line)
	if base == "" {
		return tview.Escape(line)
	}
	return base + tview.Escape(line) + "[-]"
}

func colorTag(level string) string {
	switch level {
	case "ok":
		return "[white]"
	case "warn":
		return "[yellow]"
	case "err":
		return "[red]"
	default:
		return "[white]"
	}
}
