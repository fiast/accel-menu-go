// Package service объединяет источники данных (accel, nft, docker, БД,
// /proc) в модели, которые рисует TUI, и выполняет прикладные операции.
package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"accel-menu/internal/accel"
	"accel-menu/internal/config"
	"accel-menu/internal/docker"
	"accel-menu/internal/execx"
	"accel-menu/internal/nft"
	"accel-menu/internal/procfs"
	"accel-menu/internal/store"
)

// Service — фасад приложения.
type Service struct {
	cfg   config.Config
	store *store.Store
	accel *accel.Client
	nft   *nft.Client
	dock  *docker.Client
	run   *execx.Runner
	now   func() time.Time

	mu       sync.Mutex
	prevNet  procfs.NetStats
	prevAt   time.Time
	slowAt   time.Time
	slowData slowData
	slowOK   bool

	uplink    string
	prevUpNet procfs.NetStats
	prevUpAt  time.Time
}

// New создаёт сервис.
func New(cfg config.Config, st *store.Store, ac *accel.Client, nc *nft.Client, dc *docker.Client, run *execx.Runner) *Service {
	return &Service{
		cfg: cfg, store: st, accel: ac, nft: nc, dock: dc, run: run,
		now: time.Now,
	}
}

// UplinkInfo — состояние интерфейса аплинка (default route).
type UplinkInfo struct {
	Iface     string
	OperState string
	SpeedMbps int64
	Duplex    string
	MTU       int64

	RxRate float64
	TxRate float64

	RxBytes   uint64
	TxBytes   uint64
	RxPackets uint64
	TxPackets uint64
	RxErrors  uint64
	TxErrors  uint64
	RxDropped uint64
	TxDropped uint64
}

// Dashboard — снимок для главного экрана.
type Dashboard struct {
	Now time.Time

	Load procfs.LoadAvg
	Mem  procfs.MemInfo

	MemTotalMB int64
	MemUsedMB  int64
	CPU        string
	Uptime     string
	RSSKB      int64

	Iface     string
	RxRate    float64
	TxRate    float64
	RxPackets uint64
	TxPackets uint64

	Uplink UplinkInfo

	Conntrack      int
	ConntrackTotal int
	ConntrackMax   int
	MasqIfaces     []string
	MasqIPs        []string
	IPForward      bool

	AccelState       string
	AccelEnabled     string
	AccelUptime      string
	SessionsActive   int
	SessionsStarting int
	PPPoE            int
	IPoE             int
	RadiusState      string
	RadiusFail       int

	Modules []accel.Module

	RadiusCTR string
	DBCTR     string
	DBOK      bool

	Warnings []string
	Errors   []string
}

type slowData struct {
	conntrack      int
	conntrackTotal int
	conntrackMax   int
	masq           []string
	ips            []string
	natText        string
	ipForward      bool
	dbOK           bool
	dbErr          string
	errs           []string
}

// Dashboard собирает данные для F1. Ошибки отдельных блоков не прерывают
// сборку: они попадают в Errors/Warnings.
func (s *Service) Dashboard(ctx context.Context) Dashboard {
	d := Dashboard{Now: s.now(), Iface: s.cfg.Iface}

	if l, err := procfs.ReadLoadAvg(); err != nil {
		d.Errors = append(d.Errors, "loadavg: "+err.Error())
	} else {
		d.Load = l
	}
	if m, err := procfs.ReadMemInfo(); err != nil {
		d.Errors = append(d.Errors, "meminfo: "+err.Error())
	} else {
		d.Mem = m
		d.MemTotalMB = int64(m.TotalKB / 1024)
		d.MemUsedMB = int64(m.UsedKB / 1024)
	}

	now := s.now()
	if st, err := procfs.ReadNetStats(s.cfg.Iface); err != nil {
		d.Errors = append(d.Errors, "сеть "+s.cfg.Iface+": "+err.Error())
	} else {
		s.mu.Lock()
		if !s.prevAt.IsZero() {
			dt := now.Sub(s.prevAt).Seconds()
			if dt > 0 {
				if st.RxBytes >= s.prevNet.RxBytes {
					d.RxRate = float64(st.RxBytes-s.prevNet.RxBytes) / dt
				}
				if st.TxBytes >= s.prevNet.TxBytes {
					d.TxRate = float64(st.TxBytes-s.prevNet.TxBytes) / dt
				}
			}
		}
		s.prevNet = st
		s.prevAt = now
		s.mu.Unlock()
		d.RxPackets = st.RxPackets
		d.TxPackets = st.TxPackets
	}

	// Общий счётчик conntrack читается дёшево каждые 2 с; разбивка по
	// абонентским подсетям — в медленном блоке (файл nf_conntrack большой).
	if n, err := procfs.ReadConntrackTotal(); err == nil {
		d.ConntrackTotal = int(n)
	}

	if stat, err := s.accel.ShowStat(ctx); err != nil {
		d.Errors = append(d.Errors, "accel-cmd: "+err.Error())
	} else {
		d.Uptime = stat.Uptime
		d.CPU = stat.CPU
		d.RSSKB = stat.RSSKB
		d.SessionsActive = stat.SessionsActive
		d.SessionsStarting = stat.SessionsStarting
		d.PPPoE = stat.PPPoEActive
		d.IPoE = stat.IPoEActive
		d.RadiusState = stat.RadiusState
		d.RadiusFail = stat.RadiusFail
		d.Modules = stat.Modules
	}

	d.AccelState = s.systemctl(ctx, "is-active")
	d.AccelEnabled = s.systemctl(ctx, "is-enabled")
	if d.AccelState != "active" {
		d.Warnings = append(d.Warnings, "accel-ppp не активен: "+d.AccelState)
	}
	if d.AccelEnabled == "disabled" {
		d.Warnings = append(d.Warnings, "автозагрузка accel-ppp выключена — после ребута демон не поднимется")
	}
	if d.RadiusFail > 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("RADIUS: fail count %d", d.RadiusFail))
	}

	if st, err := s.dock.Inspect(ctx, s.cfg.RadiusCTR); err != nil {
		d.RadiusCTR = "нет"
		d.Errors = append(d.Errors, "docker "+s.cfg.RadiusCTR+": "+err.Error())
	} else {
		d.RadiusCTR = st.Display()
		if !st.Running() {
			d.Warnings = append(d.Warnings, "контейнер "+s.cfg.RadiusCTR+" не запущен")
		}
	}
	if st, err := s.dock.Inspect(ctx, s.cfg.DBCTR); err != nil {
		d.DBCTR = "нет"
		d.Errors = append(d.Errors, "docker "+s.cfg.DBCTR+": "+err.Error())
	} else {
		d.DBCTR = st.Display()
		if !st.Running() {
			d.Warnings = append(d.Warnings, "контейнер "+s.cfg.DBCTR+" не запущен")
		} else if st.Health != "" && !st.Healthy() {
			d.Warnings = append(d.Warnings, "MariaDB health: "+st.Health)
		}
	}

	s.applySlow(ctx, &d)
	s.fillUplink(ctx, now, &d)
	return d
}

// fillUplink заполняет состояние аплинка: default route, sysfs-счётчики,
// скорость/дуплекс и производные скорости.
func (s *Service) fillUplink(ctx context.Context, now time.Time, d *Dashboard) {
	name := s.cfg.UplinkIface
	if name == "" {
		s.mu.Lock()
		name = s.uplink
		s.mu.Unlock()
	}
	if name == "" {
		out, err := s.run.RunSudo(ctx, "ip", "route", "show", "default")
		if err != nil {
			d.Warnings = append(d.Warnings, "uplink: не удалось определить default route: "+err.Error())
			return
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] == "dev" {
					name = fields[i+1]
					break
				}
			}
			if name != "" {
				break
			}
		}
		if name == "" {
			d.Warnings = append(d.Warnings, "uplink: в default route нет dev")
			return
		}
		s.mu.Lock()
		s.uplink = name
		s.mu.Unlock()
	}

	up := UplinkInfo{Iface: name}
	st, err := procfs.ReadNetStats(name)
	if err != nil {
		d.Errors = append(d.Errors, "uplink "+name+": "+err.Error())
		return
	}
	li, err := procfs.ReadLinkInfo(name)
	if err != nil {
		d.Warnings = append(d.Warnings, "uplink "+name+" link: "+err.Error())
	}
	up.OperState = li.OperState
	up.SpeedMbps = li.SpeedMbps
	up.Duplex = li.Duplex
	up.MTU = li.MTU
	up.RxBytes, up.TxBytes = st.RxBytes, st.TxBytes
	up.RxPackets, up.TxPackets = st.RxPackets, st.TxPackets
	up.RxErrors, up.TxErrors = st.RxErrors, st.TxErrors
	up.RxDropped, up.TxDropped = st.RxDropped, st.TxDropped

	s.mu.Lock()
	if !s.prevUpAt.IsZero() {
		dt := now.Sub(s.prevUpAt).Seconds()
		if dt > 0 {
			if st.RxBytes >= s.prevUpNet.RxBytes {
				up.RxRate = float64(st.RxBytes-s.prevUpNet.RxBytes) / dt
			}
			if st.TxBytes >= s.prevUpNet.TxBytes {
				up.TxRate = float64(st.TxBytes-s.prevUpNet.TxBytes) / dt
			}
		}
	}
	s.prevUpNet = st
	s.prevUpAt = now
	s.mu.Unlock()

	if up.OperState != "" && up.OperState != "up" {
		d.Warnings = append(d.Warnings, "uplink "+name+" в состоянии "+up.OperState)
	}
	d.Uplink = up
}

func (s *Service) applySlow(ctx context.Context, d *Dashboard) {
	s.mu.Lock()
	fresh := s.slowOK && s.now().Sub(s.slowAt) < s.cfg.SlowRefresh
	if fresh {
		sd := s.slowData
		s.mu.Unlock()
		s.applySlowData(d, sd)
		return
	}
	s.mu.Unlock()

	sd := s.collectSlow(ctx)

	s.mu.Lock()
	s.slowData = sd
	s.slowAt = s.now()
	s.slowOK = true
	s.mu.Unlock()

	s.applySlowData(d, sd)
}

func (s *Service) applySlowData(d *Dashboard, sd slowData) {
	d.Conntrack = sd.conntrack
	if d.ConntrackTotal == 0 {
		d.ConntrackTotal = sd.conntrackTotal
	}
	d.ConntrackMax = sd.conntrackMax
	d.MasqIfaces = append([]string(nil), sd.masq...)
	d.MasqIPs = append([]string(nil), sd.ips...)
	d.IPForward = sd.ipForward
	d.DBOK = sd.dbOK
	d.Errors = append(d.Errors, sd.errs...)
	if len(sd.masq) == 0 {
		d.Warnings = append(d.Warnings, "в postrouting нет правил masquerade")
	} else if !nft.HasNATFor(sd.natText, s.cfg.NetMain) {
		d.Warnings = append(d.Warnings, "нет NAT для "+s.cfg.NetMain)
	}
	if !sd.dbOK {
		d.Warnings = append(d.Warnings, "БД недоступна: "+sd.dbErr)
	}
	if !sd.ipForward {
		d.Warnings = append(d.Warnings, "ip_forward выключен")
	}
}

func (s *Service) collectSlow(ctx context.Context) slowData {
	var sd slowData

	prefixes := []string{
		"src=" + procfs.SplitCIDRPrefix(s.cfg.NetMain),
		"src=" + procfs.SplitCIDRPrefix(s.cfg.NetGuest),
	}
	if n, err := procfs.ReadConntrackCount(prefixes); err != nil {
		sd.errs = append(sd.errs, "conntrack: "+err.Error())
	} else {
		sd.conntrack = n
	}
	if n, err := procfs.ReadConntrackTotal(); err == nil {
		sd.conntrackTotal = int(n)
	} else if sd.conntrack > 0 {
		sd.conntrackTotal = sd.conntrack
	}
	if n, err := procfs.ReadConntrackMax(); err == nil {
		sd.conntrackMax = int(n)
	}

	if text, err := s.nft.Postrouting(ctx); err != nil {
		sd.errs = append(sd.errs, "nft postrouting: "+err.Error())
	} else {
		sd.natText = text
		sd.masq = nft.MasqueradeInterfaces(text)
		for _, iface := range sd.masq {
			ips, err := s.ipv4Addrs(ctx, iface)
			if err != nil {
				// Интерфейс из правила может быть удалён; это не ошибка дашборда.
				continue
			}
			sd.ips = append(sd.ips, ips...)
		}
	}

	if v, err := procfs.ReadIPForward(); err == nil {
		sd.ipForward = v
	}

	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.store.Ping(pctx); err != nil {
		sd.dbOK = false
		sd.dbErr = err.Error()
	} else {
		sd.dbOK = true
	}
	return sd
}

// ipv4Addrs — IPv4-адреса интерфейса через "ip -4 -br addr show".
func (s *Service) ipv4Addrs(ctx context.Context, iface string) ([]string, error) {
	out, err := s.run.RunSudo(ctx, "ip", "-4", "-br", "addr", "show", iface)
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		addr := fields[2]
		if i := strings.IndexByte(addr, '/'); i >= 0 {
			addr = addr[:i]
		}
		ips = append(ips, addr)
	}
	return ips, nil
}

func (s *Service) systemctl(ctx context.Context, verb string) string {
	out, _ := s.run.Run(ctx, "systemctl", verb, s.cfg.AccelUnit)
	out = strings.TrimSpace(out)
	if out == "" {
		return "unknown"
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return out
}

// ListSubscribers возвращает список абонентов.
func (s *Service) ListSubscribers(ctx context.Context) ([]store.SubscriberRow, error) {
	return s.store.ListSubscribers(ctx)
}

// Details загружает карточку.
func (s *Service) Details(ctx context.Context, login string) (store.Details, error) {
	return s.store.Details(ctx, login)
}

// Counts возвращает количества записей абонента.
func (s *Service) Counts(ctx context.Context, login string) (store.Counts, error) {
	return s.store.Counts(ctx, login)
}

// NewSubscriberInput — ввод формы создания.
type NewSubscriberInput struct {
	Username string
	Password string
	IP       string
	Down     string
	Up       string
	Group    string
	// IsIPoE выбирается в форме; true — IPoE/DHCP, false — PPPoE.
	IsIPoE bool
}

var (
	loginRe = regexp.MustCompile(`^[A-Za-z0-9._@:-]{1,64}$`)
	groupRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	ipv4Re  = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
)

// ValidateLogin проверяет логин (как bash: буквы, цифры, . _ @ : -).
func ValidateLogin(login string) error {
	if strings.TrimSpace(login) == "" {
		return errors.New("логин не может быть пустым")
	}
	if !loginRe.MatchString(login) {
		return errors.New("недопустимые символы в логине (разрешены A-Z a-z 0-9 . _ @ : -)")
	}
	return nil
}

// ValidateIPv4 проверяет необязательный IPv4-адрес.
func ValidateIPv4(ip string) error {
	if ip == "" {
		return nil
	}
	if !ipv4Re.MatchString(ip) {
		return fmt.Errorf("некорректный IPv4: %s", ip)
	}
	for _, part := range strings.Split(ip, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return fmt.Errorf("некорректный IPv4: %s", ip)
		}
	}
	return nil
}

// ValidateSpeed проверяет скорость в кбит/с.
func ValidateSpeed(v string) error {
	n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
	if err != nil || n == 0 || n > 100_000_000 {
		return fmt.Errorf("некорректная скорость %q (1..100000000 кбит/с)", v)
	}
	return nil
}

// ValidateGroup проверяет имя группы.
func ValidateGroup(v string) error {
	if v == "" {
		return nil
	}
	if !groupRe.MatchString(v) {
		return fmt.Errorf("некорректная группа %q", v)
	}
	return nil
}

// CreateSubscriber валидирует и создаёт абонента.
func (s *Service) CreateSubscriber(ctx context.Context, in NewSubscriberInput) error {
	login := strings.TrimSpace(in.Username)
	if err := ValidateLogin(login); err != nil {
		return err
	}
	isIPoE := in.IsIPoE
	if isIPoE && !store.IsMAC(login) {
		return errors.New("для типа IPoE логин должен быть MAC-адресом (aa:bb:cc:dd:ee:ff)")
	}

	password := in.Password
	if isIPoE {
		password = strings.ReplaceAll(login, ":", "")
	} else if strings.TrimSpace(password) == "" {
		return errors.New("для PPPoE пароль обязателен")
	}
	if len(password) > 128 {
		return errors.New("пароль слишком длинный (максимум 128 символов)")
	}

	ip := strings.TrimSpace(in.IP)
	if err := ValidateIPv4(ip); err != nil {
		return err
	}
	down := strings.TrimSpace(in.Down)
	if down == "" {
		down = "50000"
	}
	up := strings.TrimSpace(in.Up)
	if up == "" {
		up = "20000"
	}
	if err := ValidateSpeed(down); err != nil {
		return err
	}
	if err := ValidateSpeed(up); err != nil {
		return err
	}
	group := strings.TrimSpace(in.Group)
	if group == "" {
		group = "default"
	}
	if err := ValidateGroup(group); err != nil {
		return err
	}

	return s.store.Create(ctx, store.NewSubscriber{
		Username: login,
		Password: password,
		IP:       ip,
		Down:     down,
		Up:       up,
		Group:    group,
		IsIPoE:   isIPoE,
	})
}

// DeleteResult — итог удаления.
type DeleteResult struct {
	Counts           store.Counts
	TerminateOutput  string
	TerminateWarning string
}

// DeleteSubscriber завершает сессию и удаляет записи абонента.
func (s *Service) DeleteSubscriber(ctx context.Context, login string) (DeleteResult, error) {
	var res DeleteResult
	counts, err := s.store.Counts(ctx, login)
	if err != nil {
		return res, err
	}
	res.Counts = counts

	out, terr := s.accel.TerminateUser(ctx, login)
	res.TerminateOutput = strings.TrimSpace(out)
	if terr != nil {
		// Нет активной сессии — accel-cmd возвращает ошибку; удалять это не мешает.
		res.TerminateWarning = terr.Error()
	}
	if err := s.store.Delete(ctx, login); err != nil {
		return res, err
	}
	return res, nil
}

// Sessions возвращает таблицу активных сессий accel-ppp.
func (s *Service) Sessions(ctx context.Context) ([]accel.Session, error) {
	return s.accel.ShowSessions(ctx)
}

// TerminateSession мягко завершает сессию по имени интерфейса.
func (s *Service) TerminateSession(ctx context.Context, ifname string) (string, error) {
	ifname = strings.TrimSpace(ifname)
	if ifname == "" {
		return "", errors.New("пустое имя интерфейса")
	}
	return s.accel.TerminateInterface(ctx, ifname)
}

// ReloadAccel перечитывает конфигурацию без разрыва сессий.
func (s *Service) ReloadAccel(ctx context.Context) (string, error) {
	return s.run.RunSudoTimeout(ctx, 15*time.Second, "systemctl", "reload", s.cfg.AccelUnit)
}

// RestartAccel перезапускает демон (сессии разрываются).
func (s *Service) RestartAccel(ctx context.Context) (string, error) {
	return s.run.RunSudoTimeout(ctx, 30*time.Second, "systemctl", "restart", s.cfg.AccelUnit)
}

// HardRestartAccel убивает процесс SIGKILL и запускает заново.
func (s *Service) HardRestartAccel(ctx context.Context) (string, error) {
	killOut, killErr := s.run.RunSudoTimeout(ctx, 10*time.Second,
		"systemctl", "kill", "-s", "SIGKILL", s.cfg.AccelUnit)
	out, err := s.run.RunSudoTimeout(ctx, 30*time.Second,
		"systemctl", "restart", s.cfg.AccelUnit)
	if err != nil {
		return strings.TrimSpace(killOut + "\n" + out), fmt.Errorf("restart: %w", err)
	}
	res := strings.TrimSpace(out)
	if killErr != nil {
		res = strings.TrimSpace(res + "\nkill: " + killErr.Error())
	}
	return res, nil
}

// NFTSet — set для экрана блокировок.
type NFTSet struct {
	Name    string
	Desc    string
	Count   int
	Extra   bool
	Missing bool
}

// NFTMatch — найденный IP и set, в котором он лежит.
type NFTMatch struct {
	Set string
	IP  string
}

// FindIP ищет IP во всех set-ах inet filter. Порядок set-ов совпадает
// с экраном F3: сначала штатные, затем обнаруженные.
func (s *Service) FindIP(ctx context.Context, ip string) ([]NFTMatch, error) {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, fmt.Errorf("некорректный IP: %s", ip)
	}
	ip = parsed.String()

	sets, err := s.nft.Sets(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]nft.Set, len(sets))
	for _, set := range sets {
		byName[set.Name] = set
	}

	var matches []NFTMatch
	known := make(map[string]struct{}, len(s.cfg.ACLSets))
	check := func(name string) {
		set, ok := byName[name]
		if !ok {
			return
		}
		for _, e := range set.Elements {
			if e == ip {
				matches = append(matches, NFTMatch{Set: name, IP: ip})
				return
			}
		}
	}
	for _, acl := range s.cfg.ACLSets {
		known[acl.Name] = struct{}{}
		check(acl.Name)
	}
	for _, set := range sets {
		if _, ok := known[set.Name]; ok {
			continue
		}
		if !strings.HasPrefix(set.Name, "ipset_") && !strings.HasPrefix(set.Name, "allowed_") {
			continue
		}
		check(set.Name)
	}
	return matches, nil
}

// NFTSets возвращает штатные set-ы + обнаруженные дополнительные.
func (s *Service) NFTSets(ctx context.Context) ([]NFTSet, error) {
	sets, err := s.nft.Sets(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]nft.Set, len(sets))
	for _, set := range sets {
		byName[set.Name] = set
	}
	var out []NFTSet
	known := make(map[string]struct{}, len(s.cfg.ACLSets))
	for _, acl := range s.cfg.ACLSets {
		known[acl.Name] = struct{}{}
		item := NFTSet{Name: acl.Name, Desc: acl.Desc}
		if set, ok := byName[acl.Name]; ok {
			item.Count = set.Count
		} else {
			item.Missing = true
		}
		out = append(out, item)
	}
	for _, set := range sets {
		if _, ok := known[set.Name]; ok {
			continue
		}
		if !strings.HasPrefix(set.Name, "ipset_") && !strings.HasPrefix(set.Name, "allowed_") {
			continue
		}
		out = append(out, NFTSet{
			Name: set.Name, Desc: "прочий set (обнаружен)", Count: set.Count, Extra: true,
		})
	}
	return out, nil
}

// NFTSetElements возвращает элементы set-а.
func (s *Service) NFTSetElements(ctx context.Context, name string) ([]nft.Element, error) {
	return s.nft.Elements(ctx, name)
}

// FlushSet очищает set.
func (s *Service) FlushSet(ctx context.Context, name string) error {
	return s.nft.Flush(ctx, name)
}

// DeleteElement удаляет IP из set-а.
func (s *Service) DeleteElement(ctx context.Context, name, ip string) error {
	return s.nft.DeleteElement(ctx, name, ip)
}

// LogTail читает последние строки лога accel-ppp.
func (s *Service) LogTail(ctx context.Context) ([]string, error) {
	return readTail(s.cfg.LogPath, s.cfg.TailBytes, s.cfg.TailLines)
}

func readTail(path string, maxBytes int64, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var lines []string
	first := offset > 0
	for sc.Scan() {
		line := sc.Text()
		if first {
			// Первая строка может быть обрезана — выбрасываем.
			first = false
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

// HealthText — короткий статус для статус-строки.
func HealthText(d Dashboard) string {
	if len(d.Errors) == 0 && len(d.Warnings) == 0 {
		return "все компоненты в норме"
	}
	return fmt.Sprintf("предупреждений: %d, ошибок: %d", len(d.Warnings), len(d.Errors))
}
