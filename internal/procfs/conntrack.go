package procfs

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ConntrackEntry — одна запись /proc/net/nf_conntrack в виде, пригодном
// для таблицы. Поля Orig/Reply описывают прямое и обратное направления.
type ConntrackEntry struct {
	Key string

	Status   string
	Src      string
	SrcPort  int
	Dst      string
	DstPort  int
	ReplySrc string
	ReplyDst string

	Proto    string
	ProtoNum int
	Timeout  int
	TCPState string

	OrigBytes    uint64
	ReplyBytes   uint64
	OrigPackets  uint64
	ReplyPackets uint64

	// OrigRate/ReplyRate заполняет UI по разнице счётчиков между обновлениями.
	OrigRate  float64
	ReplyRate float64
}

// FormatTimeout форматирует секунды в HH:MM:SS, как в conntrack-таблицах.
func FormatTimeout(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// ParseConntrackLine разбирает одну строку nf_conntrack.
// Формат строки:
//
//	ipv4 2 tcp 6 95 TIME_WAIT src=... dst=... sport=... dport=... \
//	     src=... dst=... sport=... dport=... [ASSURED] mark=0 ...
func ParseConntrackLine(line string) (ConntrackEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 || (fields[0] != "ipv4" && fields[0] != "ipv6") {
		return ConntrackEntry{}, false
	}
	protoNum, err := strconv.Atoi(fields[3])
	if err != nil {
		return ConntrackEntry{}, false
	}
	timeout, _ := strconv.Atoi(fields[4])
	e := ConntrackEntry{
		Proto:    fields[2],
		ProtoNum: protoNum,
		Timeout:  timeout,
	}
	i := 5
	// У TCP после timeout идёт TCP-состояние. У UDP/ICMP сразу ключи.
	if i < len(fields) && !strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "[") {
		e.TCPState = fields[i]
		i++
	}

	var origSrcSet, replySrcSet bool
	var origBytesSet, replyBytesSet bool
	var origPktsSet, replyPktsSet bool
	assured, unreplied := false, false
	for ; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "[ASSURED]":
			assured = true
		case f == "[UNREPLIED]":
			unreplied = true
		case strings.HasPrefix(f, "src="):
			v := strings.TrimPrefix(f, "src=")
			if !origSrcSet {
				e.Src, origSrcSet = v, true
			} else if !replySrcSet {
				e.ReplySrc, replySrcSet = v, true
			}
		case strings.HasPrefix(f, "dst="):
			v := strings.TrimPrefix(f, "dst=")
			if e.Dst == "" {
				e.Dst = v
			} else if e.ReplyDst == "" {
				e.ReplyDst = v
			}
		case strings.HasPrefix(f, "sport="):
			v, _ := strconv.Atoi(strings.TrimPrefix(f, "sport="))
			if e.SrcPort == 0 {
				e.SrcPort = v
			}
		case strings.HasPrefix(f, "dport="):
			v, _ := strconv.Atoi(strings.TrimPrefix(f, "dport="))
			if e.DstPort == 0 {
				e.DstPort = v
			}
		case strings.HasPrefix(f, "bytes="):
			v, _ := strconv.ParseUint(strings.TrimPrefix(f, "bytes="), 10, 64)
			if !origBytesSet {
				e.OrigBytes, origBytesSet = v, true
			} else if !replyBytesSet {
				e.ReplyBytes, replyBytesSet = v, true
			}
		case strings.HasPrefix(f, "packets="):
			v, _ := strconv.ParseUint(strings.TrimPrefix(f, "packets="), 10, 64)
			if !origPktsSet {
				e.OrigPackets, origPktsSet = v, true
			} else if !replyPktsSet {
				e.ReplyPackets, replyPktsSet = v, true
			}
		}
	}
	if e.Src == "" || e.Dst == "" {
		return ConntrackEntry{}, false
	}

	status := ""
	if replySrcSet && !unreplied {
		status += "S"
	}
	if assured {
		status += "A"
	}
	status += "C"
	e.Status = status
	e.Key = fmt.Sprintf("%s:%d-%s:%d/%s", e.Src, e.SrcPort, e.Dst, e.DstPort, e.Proto)
	return e, true
}

// ConntrackMatchesIP сообщает, участвует ли IP в записи (в любом направлении).
func ConntrackMatchesIP(e ConntrackEntry, ip string) bool {
	return e.Src == ip || e.Dst == ip || e.ReplySrc == ip || e.ReplyDst == ip
}

// ReadConntrackForIP читает /proc/net/nf_conntrack и оставляет записи,
// относящиеся к указанному IP. Пустой IP означает все записи.
func ReadConntrackForIP(ip string) ([]ConntrackEntry, error) {
	return ReadConntrackForIPAt("/proc/net/nf_conntrack", ip)
}

// ReadConntrackForIPAt — версия для тестов.
func ReadConntrackForIPAt(path, ip string) ([]ConntrackEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ip = strings.TrimSpace(ip)
	var out []ConntrackEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	const maxLines = 1_000_000
	lines := 0
	for sc.Scan() {
		lines++
		if lines > maxLines {
			break
		}
		e, ok := ParseConntrackLine(sc.Text())
		if !ok {
			continue
		}
		if ip != "" && !ConntrackMatchesIP(e, ip) {
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Src != b.Src {
			return a.Src < b.Src
		}
		if a.SrcPort != b.SrcPort {
			return a.SrcPort < b.SrcPort
		}
		if a.Dst != b.Dst {
			return a.Dst < b.Dst
		}
		if a.DstPort != b.DstPort {
			return a.DstPort < b.DstPort
		}
		return a.Proto < b.Proto
	})
	return out, nil
}
