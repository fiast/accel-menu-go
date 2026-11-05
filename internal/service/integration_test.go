//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"accel-menu/internal/accel"
	"accel-menu/internal/config"
	"accel-menu/internal/docker"
	"accel-menu/internal/execx"
	"accel-menu/internal/nft"
	"accel-menu/internal/store"
)

// TestCreateDeleteIntegration проверяет полный путь сервиса:
// валидация -> INSERT в три таблицы -> чтение карточки -> удаление.
// Запускается через SSH-туннель к стенду (см. README).
func TestCreateDeleteIntegration(t *testing.T) {
	dsn := os.Getenv("BRAS_TEST_DSN")
	if dsn == "" {
		t.Skip("BRAS_TEST_DSN не задан")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := config.Defaults()
	st := store.NewFromDB(db, cfg)
	run := execx.New(false, 2*time.Second)
	svc := New(cfg, st,
		accel.New(run, cfg.AccelCmd),
		nft.New(run, cfg.NFTFamily, cfg.NFTTable),
		docker.New(run), run)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	login := fmt.Sprintf("zz-svc-%d", time.Now().UnixNano())
	in := NewSubscriberInput{
		Username: login,
		Password: "testpass123",
		Down:     "1000",
		Up:       "1000",
		Group:    "default",
	}
	if err := svc.CreateSubscriber(ctx, in); err != nil {
		t.Fatalf("CreateSubscriber: %v", err)
	}
	defer func() {
		_ = st.Delete(context.Background(), login)
	}()

	d, err := svc.Details(ctx, login)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	if len(d.Checks) == 0 || len(d.Replies) == 0 || len(d.Groups) == 0 {
		t.Fatalf("неполная карточка: %+v", d)
	}

	res, err := svc.DeleteSubscriber(ctx, login)
	if err != nil {
		t.Fatalf("DeleteSubscriber: %v", err)
	}
	_ = res // TerminateWarning ожидаем при отсутствии accel-cmd; это не ошибка.

	exists, err := st.Exists(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("абонент остался в radcheck")
	}
}
