// Package store — доступ к базе FreeRADIUS (MariaDB/MySQL).
//
// Все запросы идут через placeholders, пароли никогда не логируются.
// Схема БД не меняется: приложение только читает и пишет данные,
// которые ожидает FreeRADIUS.
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"accel-menu/internal/config"
)

// ErrExists — абонент уже есть в radcheck.
var ErrExists = errors.New("абонент уже существует")

// ErrNotFound — записей абонента нет.
var ErrNotFound = errors.New("абонент не найден")

// SubscriberRow — строка списка абонентов.
type SubscriberRow struct {
	Username string
	IsIPoE   bool
	HasNT    bool
	// IP — статический адрес из radreply либо последний адрес из radacct.
	IP string
}

// CheckRow — запись radcheck.
type CheckRow struct {
	Attribute string
	Value     string
}

// ReplyRow — запись radreply.
type ReplyRow struct {
	Attribute string
	Op        string
	Value     string
}

// GroupRow — запись radusergroup.
type GroupRow struct {
	Group    string
	Priority int
}

// AcctRow — запись radacct.
type AcctRow struct {
	Start   sql.NullTime
	Stop    sql.NullTime
	IP      string
	Seconds sql.NullInt64
	Cause   string
}

// AuthRow — запись radpostauth.
type AuthRow struct {
	Date  time.Time
	Reply string
}

// Details — карточка абонента.
type Details struct {
	Username string
	IsIPoE   bool
	Checks   []CheckRow
	Replies  []ReplyRow
	Groups   []GroupRow
	Sessions []AcctRow
	Auths    []AuthRow
}

// Counts — количество записей абонента по таблицам.
type Counts struct {
	Check int
	Reply int
	Group int
	Acct  int
}

// TableCounts — общие счётчики для диагностики.
type TableCounts struct {
	Users   int
	Checks  int
	Replies int
	Acct    int
	Auths   int
}

// NewSubscriber — данные для создания абонента.
type NewSubscriber struct {
	Username string
	Password string
	IP       string
	Down     string
	Up       string
	Group    string
	IsIPoE   bool
}

// Store — обёртка над *sql.DB.
type Store struct {
	db  *sql.DB
	cfg config.Config
}

// Open создаёт пул соединений. Соединение не устанавливается до Ping/запроса.
func Open(cfg config.Config) (*Store, error) {
	c := mysql.NewConfig()
	c.User = cfg.DBUser
	c.Passwd = cfg.DBPass
	c.Net = "tcp"
	c.Addr = cfg.DBHost
	c.DBName = cfg.DBName
	c.ParseTime = true
	c.Loc = time.Local
	c.Timeout = 3 * time.Second
	c.ReadTimeout = 5 * time.Second
	c.WriteTimeout = 5 * time.Second
	c.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Store{db: db, cfg: cfg}, nil
}

// NewFromDB — конструктор для тестов.
func NewFromDB(db *sql.DB, cfg config.Config) *Store { return &Store{db: db, cfg: cfg} }

// Close закрывает пул.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Ping проверяет доступность БД.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ListSubscribers возвращает всех абонентов, признак NT-Password и IP.
// IP берётся из radreply (статический) либо из последней сессии radacct.
func (s *Store) ListSubscribers(ctx context.Context) ([]SubscriberRow, error) {
	const q = `SELECT username, MAX(attribute = 'NT-Password')
	           FROM radcheck
	           GROUP BY username
	           ORDER BY username`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}

	var out []SubscriberRow
	for rows.Next() {
		var name string
		var nt int
		if err := rows.Scan(&name, &nt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SubscriberRow{
			Username: name,
			IsIPoE:   IsMAC(name),
			HasNT:    nt != 0,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	static, err := s.staticIPs(ctx)
	if err != nil {
		return nil, err
	}
	last, err := s.lastIPs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if ip := static[out[i].Username]; ip != "" {
			out[i].IP = ip
		} else {
			out[i].IP = last[out[i].Username]
		}
	}
	return out, nil
}

// staticIPs — статические адреса из radreply.
func (s *Store) staticIPs(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT username, value FROM radreply
		 WHERE attribute IN ('Framed-IP-Address', 'DHCP-Your-IP-Address')
		 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var user, ip string
		if err := rows.Scan(&user, &ip); err != nil {
			return nil, err
		}
		if _, ok := out[user]; !ok {
			out[user] = ip
		}
	}
	return out, rows.Err()
}

// lastIPs — адрес из последней сессии radacct по каждому логину.
func (s *Store) lastIPs(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.username, a.framedipaddress
		 FROM radacct a
		 JOIN (SELECT username, MAX(radacctid) AS id FROM radacct GROUP BY username) m
		   ON m.id = a.radacctid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var user, ip string
		if err := rows.Scan(&user, &ip); err != nil {
			return nil, err
		}
		out[user] = ip
	}
	return out, rows.Err()
}

// Details загружает карточку абонента.
func (s *Store) Details(ctx context.Context, login string) (Details, error) {
	d := Details{Username: login, IsIPoE: IsMAC(login)}

	rows, err := s.db.QueryContext(ctx, `SELECT attribute, value FROM radcheck WHERE username = ? ORDER BY id`, login)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var c CheckRow
		if err := rows.Scan(&c.Attribute, &c.Value); err != nil {
			rows.Close()
			return d, err
		}
		d.Checks = append(d.Checks, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return d, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT attribute, op, value FROM radreply WHERE username = ? ORDER BY id`, login)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var r ReplyRow
		if err := rows.Scan(&r.Attribute, &r.Op, &r.Value); err != nil {
			rows.Close()
			return d, err
		}
		d.Replies = append(d.Replies, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return d, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT groupname, priority FROM radusergroup WHERE username = ? ORDER BY priority`, login)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var g GroupRow
		if err := rows.Scan(&g.Group, &g.Priority); err != nil {
			rows.Close()
			return d, err
		}
		d.Groups = append(d.Groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return d, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx,
		`SELECT acctstarttime, acctstoptime, framedipaddress, acctsessiontime, acctterminatecause
		 FROM radacct WHERE username = ?
		 ORDER BY radacctid DESC LIMIT 5`, login)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var a AcctRow
		var cause sql.NullString
		if err := rows.Scan(&a.Start, &a.Stop, &a.IP, &a.Seconds, &cause); err != nil {
			rows.Close()
			return d, err
		}
		a.Cause = cause.String
		d.Sessions = append(d.Sessions, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return d, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx,
		`SELECT authdate, reply FROM radpostauth WHERE username = ? ORDER BY id DESC LIMIT 5`, login)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var a AuthRow
		if err := rows.Scan(&a.Date, &a.Reply); err != nil {
			rows.Close()
			return d, err
		}
		d.Auths = append(d.Auths, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return d, err
	}
	rows.Close()

	if len(d.Checks) == 0 && len(d.Replies) == 0 && len(d.Groups) == 0 {
		return d, ErrNotFound
	}
	return d, nil
}

// Counts возвращает количество записей абонента.
func (s *Store) Counts(ctx context.Context, login string) (Counts, error) {
	const q = `SELECT
	  (SELECT COUNT(*) FROM radcheck WHERE username = ?),
	  (SELECT COUNT(*) FROM radreply WHERE username = ?),
	  (SELECT COUNT(*) FROM radusergroup WHERE username = ?),
	  (SELECT COUNT(*) FROM radacct WHERE username = ?)`
	var c Counts
	err := s.db.QueryRowContext(ctx, q, login, login, login, login).
		Scan(&c.Check, &c.Reply, &c.Group, &c.Acct)
	return c, err
}

// Stats возвращает глобальные счётчики таблиц.
func (s *Store) Stats(ctx context.Context) (TableCounts, error) {
	const q = `SELECT
	  (SELECT COUNT(DISTINCT username) FROM radcheck),
	  (SELECT COUNT(*) FROM radcheck),
	  (SELECT COUNT(*) FROM radreply),
	  (SELECT COUNT(*) FROM radacct),
	  (SELECT COUNT(*) FROM radpostauth)`
	var c TableCounts
	err := s.db.QueryRowContext(ctx, q).Scan(&c.Users, &c.Checks, &c.Replies, &c.Acct, &c.Auths)
	return c, err
}

// Exists проверяет наличие логина в radcheck.
func (s *Store) Exists(ctx context.Context, login string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM radcheck WHERE username = ?`, login).Scan(&n)
	return n > 0, err
}

// Create создаёт абонента одной транзакцией.
func (s *Store) Create(ctx context.Context, in NewSubscriber) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM radcheck WHERE username = ?`, in.Username).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrExists
	}

	password := in.Password
	if in.IsIPoE {
		password = strings.ReplaceAll(in.Username, ":", "")
	}
	if password == "" {
		return errors.New("пустой пароль")
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO radcheck (username, password, attribute, op, value)
		 VALUES (?, ?, 'Cleartext-Password', ':=', ?)`,
		in.Username, password, password); err != nil {
		return err
	}

	type reply struct{ attr, op, value string }
	var replies []reply
	speedDown, speedUp := in.Down, in.Up
	if speedDown == "" {
		speedDown = "50000"
	}
	if speedUp == "" {
		speedUp = "20000"
	}
	gateway := "10.92.0.1"

	switch {
	case in.IsIPoE && in.IP != "":
		replies = []reply{
			{"DHCP-Your-IP-Address", "=", in.IP},
			{"Framed-IP-Netmask", "=", "255.255.255.0"},
			{"DHCP-Router-Address", "=", gateway},
			{"Max-Download-Speed", "=", speedDown},
			{"Max-Upload-Speed", "=", speedUp},
		}
	case in.IsIPoE:
		replies = []reply{
			{"Max-Download-Speed", "=", speedDown},
			{"Max-Upload-Speed", "=", speedUp},
		}
	default:
		replies = []reply{
			{"Framed-Pool", "=", "main"},
			{"Framed-Router", "=", gateway},
			{"Max-Download-Speed", "=", speedDown},
			{"Max-Upload-Speed", "=", speedUp},
		}
	}
	for _, r := range replies {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO radreply (username, attribute, op, value) VALUES (?, ?, ?, ?)`,
			in.Username, r.attr, r.op, r.value); err != nil {
			return err
		}
	}

	group := strings.TrimSpace(in.Group)
	if group == "" {
		group = "default"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO radusergroup (username, groupname, priority) VALUES (?, ?, 0)`,
		in.Username, group); err != nil {
		return err
	}

	return tx.Commit()
}

// Delete удаляет абонента из radcheck/radreply/radusergroup.
// История radacct не удаляется.
func (s *Store) Delete(ctx context.Context, login string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var n1, n2, n3 int
	if err := tx.QueryRowContext(ctx,
		`SELECT
		   (SELECT COUNT(*) FROM radcheck WHERE username = ?),
		   (SELECT COUNT(*) FROM radreply WHERE username = ?),
		   (SELECT COUNT(*) FROM radusergroup WHERE username = ?)`,
		login, login, login).Scan(&n1, &n2, &n3); err != nil {
		return err
	}
	if n1+n2+n3 == 0 {
		return ErrNotFound
	}
	for _, q := range []string{
		`DELETE FROM radcheck WHERE username = ?`,
		`DELETE FROM radreply WHERE username = ?`,
		`DELETE FROM radusergroup WHERE username = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, login); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IsMAC проверяет, похож ли логин на MAC-адрес (IPoE).
func IsMAC(login string) bool {
	if len(login) != 17 {
		return false
	}
	for i, r := range login {
		switch i % 3 {
		case 2:
			if r != ':' {
				return false
			}
		default:
			if !isHex(r) {
				return false
			}
		}
	}
	return true
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
