//go:build integration

package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"accel-menu/internal/config"
)

// TestCRUDIntegration проверяет Create/Details/Delete на реальной БД.
// Запуск (пароль берётся из окружения, в коде его нет):
//
//	BRAS_TEST_DSN="${BRAS_DB_USER}:${BRAS_DB_PASS}@tcp(127.0.0.1:13306)/radius?parseTime=true" \
//	  go test -tags=integration ./internal/store -run TestCRUDIntegration -v
//
// Тест создаёт временного абонента zz-it-<timestamp> и обязательно удаляет его.
func TestCRUDIntegration(t *testing.T) {
	dsn := os.Getenv("BRAS_TEST_DSN")
	if dsn == "" {
		t.Skip("BRAS_TEST_DSN не задан")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st := NewFromDB(db, config.Defaults())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	login := fmt.Sprintf("zz-it-%d", time.Now().UnixNano())
	in := NewSubscriber{
		Username: login,
		Password: "testpass123",
		Down:     "1000",
		Up:       "1000",
		Group:    "default",
	}
	if err := st.Create(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		_ = st.Delete(context.Background(), login)
	}()

	d, err := st.Details(ctx, login)
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	if len(d.Checks) == 0 || len(d.Replies) == 0 || len(d.Groups) == 0 {
		t.Fatalf("неполная карточка: %+v", d)
	}
	if d.Replies[0].Attribute == "" {
		t.Fatalf("пустой radreply: %+v", d.Replies)
	}

	c, err := st.Counts(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if c.Check == 0 || c.Reply == 0 || c.Group == 0 {
		t.Fatalf("counts=%+v", c)
	}

	if err := st.Delete(ctx, login); err != nil {
		t.Fatalf("delete: %v", err)
	}
	exists, err := st.Exists(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("абонент остался в radcheck после удаления")
	}
}

// TestCreateIPoEIntegration проверяет статический IP для MAC-логина:
// в radreply должны появиться DHCP-атрибуты.
func TestCreateIPoEIntegration(t *testing.T) {
	dsn := os.Getenv("BRAS_TEST_DSN")
	if dsn == "" {
		t.Skip("BRAS_TEST_DSN не задан")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st := NewFromDB(db, config.Defaults())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	n := time.Now().UnixNano()
	login := fmt.Sprintf("02:00:%02x:%02x:%02x:%02x",
		byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	if !IsMAC(login) {
		t.Fatalf("тестовый логин не MAC: %s", login)
	}
	in := NewSubscriber{
		Username: login,
		IP:       "192.0.2.250",
		Down:     "1000",
		Up:       "1000",
		Group:    "default",
		IsIPoE:   true,
	}
	if err := st.Create(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		_ = st.Delete(context.Background(), login)
	}()

	d, err := st.Details(ctx, login)
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	found := false
	for _, r := range d.Replies {
		if r.Attribute == "DHCP-Your-IP-Address" && r.Value == "192.0.2.250" {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет DHCP-Your-IP-Address: %+v", d.Replies)
	}
	list, err := st.ListSubscribers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listedIP string
	for _, row := range list {
		if row.Username == login {
			listedIP = row.IP
			break
		}
	}
	if listedIP != "192.0.2.250" {
		t.Fatalf("IP в списке = %q", listedIP)
	}
	if err := st.Delete(ctx, login); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
