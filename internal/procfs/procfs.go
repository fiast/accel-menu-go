// Package procfs читает /proc и sysfs: loadavg, память, счётчики сети,
// модули ядра, conntrack.
package procfs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadAvg — средняя загрузка системы.
type LoadAvg struct {
	One     float64
	Five    float64
	Fifteen float64
}

// ParseLoadAvg разбирает содержимое /proc/loadavg.
func ParseLoadAvg(data string) (LoadAvg, error) {
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return LoadAvg{}, fmt.Errorf("loadavg: мало полей (%d)", len(fields))
	}
	var l LoadAvg
	vals := []*float64{&l.One, &l.Five, &l.Fifteen}
	for i := range vals {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return LoadAvg{}, fmt.Errorf("loadavg: %w", err)
		}
		*vals[i] = v
	}
	return l, nil
}

// ReadLoadAvg читает /proc/loadavg.
func ReadLoadAvg() (LoadAvg, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return LoadAvg{}, err
	}
	return ParseLoadAvg(string(data))
}

// MemInfo — память в килобайтах.
type MemInfo struct {
	TotalKB     uint64
	AvailableKB uint64
	UsedKB      uint64
	UsedPercent float64
}

// ParseMemInfo разбирает /proc/meminfo. Если MemAvailable отсутствует
// (старые ядра), используется MemFree + Buffers + Cached.
func ParseMemInfo(data string) (MemInfo, error) {
	values := make(map[string]uint64)
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		key := line[:i]
		fields := strings.Fields(line[i+1:])
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = n
	}
	if err := sc.Err(); err != nil {
		return MemInfo{}, err
	}
	total, ok := values["MemTotal"]
	if !ok || total == 0 {
		return MemInfo{}, errors.New("meminfo: нет MemTotal")
	}
	avail, ok := values["MemAvailable"]
	if !ok {
		avail = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if avail > total {
		avail = total
	}
	used := total - avail
	return MemInfo{
		TotalKB:     total,
		AvailableKB: avail,
		UsedKB:      used,
		UsedPercent: float64(used) / float64(total) * 100,
	}, nil
}

// ReadMemInfo читает /proc/meminfo.
func ReadMemInfo() (MemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemInfo{}, err
	}
	return ParseMemInfo(string(data))
}

// NetStats — счётчики сетевого интерфейса.
type NetStats struct {
	RxBytes   uint64
	TxBytes   uint64
	RxPackets uint64
	TxPackets uint64
}

// ReadNetStats читает счётчики из /sys/class/net/<iface>/statistics.
func ReadNetStats(iface string) (NetStats, error) {
	return ReadNetStatsAt("/sys/class/net", iface)
}

// ReadNetStatsAt — версия для тестов с произвольным корнем sysfs.
func ReadNetStatsAt(root, iface string) (NetStats, error) {
	base := fmt.Sprintf("%s/%s/statistics", strings.TrimRight(root, "/"), iface)
	read := func(name string) (uint64, error) {
		data, err := os.ReadFile(base + "/" + name)
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	}
	var st NetStats
	var err error
	if st.RxBytes, err = read("rx_bytes"); err != nil {
		return NetStats{}, err
	}
	if st.TxBytes, err = read("tx_bytes"); err != nil {
		return NetStats{}, err
	}
	if st.RxPackets, err = read("rx_packets"); err != nil {
		return NetStats{}, err
	}
	if st.TxPackets, err = read("tx_packets"); err != nil {
		return NetStats{}, err
	}
	return st, nil
}

// ReadModules возвращает имена загруженных модулей ядра.
func ReadModules() (map[string]struct{}, error) {
	return ReadModulesAt("/proc/modules")
}

// ReadModulesAt — версия для тестов.
func ReadModulesAt(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) > 0 {
			out[fields[0]] = struct{}{}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadUintFile читает одно целое число из файла (sysctl/sysfs).
func ReadUintFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// ReadIPForward читает /proc/sys/net/ipv4/ip_forward.
func ReadIPForward() (bool, error) {
	v, err := ReadUintFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// ReadConntrackMax читает лимит таблицы conntrack.
func ReadConntrackMax() (uint64, error) {
	return ReadUintFile("/proc/sys/net/netfilter/nf_conntrack_max")
}

// ReadConntrackTotal читает общее число записей conntrack (дешёвый sysctl).
func ReadConntrackTotal() (uint64, error) {
	return ReadUintFile("/proc/sys/net/netfilter/nf_conntrack_count")
}

// ReadConntrackCount считает строки /proc/net/nf_conntrack, содержащие
// любой из префиксов (например, "src=10.92."). Возвращает число записей.
func ReadConntrackCount(prefixes []string) (int, error) {
	return ReadConntrackCountAt("/proc/net/nf_conntrack", prefixes)
}

// ReadConntrackCountAt — версия для тестов.
func ReadConntrackCountAt(path string, prefixes []string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	lines := 0
	const maxLines = 2_000_000
	for sc.Scan() {
		lines++
		if lines > maxLines {
			break
		}
		line := sc.Text()
		for _, p := range prefixes {
			if p != "" && strings.Contains(line, p) {
				n++
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return n, err
	}
	return n, nil
}

// SplitCIDRPrefix превращает "10.92.0.0/24" в "10.92." — префикс для
// поиска src= в conntrack. Для нестандартных масок возвращает первые
// три октета.
func SplitCIDRPrefix(cidr string) string {
	ip := strings.TrimSpace(cidr)
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	parts := strings.Split(ip, ".")
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1] + "." + parts[2] + "."
	}
	return ip + "."
}
