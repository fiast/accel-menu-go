package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"accel-menu/internal/accel"
	"accel-menu/internal/config"
	"accel-menu/internal/nft"
	"accel-menu/internal/service"
	"accel-menu/internal/store"
)

type fakeService struct{}

func (fakeService) Dashboard(context.Context) service.Dashboard { return service.Dashboard{} }
func (fakeService) ListSubscribers(context.Context) ([]store.SubscriberRow, error) {
	return nil, nil
}
func (fakeService) Details(context.Context, string) (store.Details, error) {
	return store.Details{}, nil
}
func (fakeService) Counts(context.Context, string) (store.Counts, error) { return store.Counts{}, nil }
func (fakeService) CreateSubscriber(context.Context, service.NewSubscriberInput) error {
	return nil
}
func (fakeService) DeleteSubscriber(context.Context, string) (service.DeleteResult, error) {
	return service.DeleteResult{}, nil
}
func (fakeService) NFTSets(context.Context) ([]service.NFTSet, error) { return nil, nil }
func (fakeService) NFTSetElements(context.Context, string) ([]nft.Element, error) {
	return nil, nil
}
func (fakeService) FindIP(context.Context, string) ([]service.NFTMatch, error) { return nil, nil }
func (fakeService) FlushSet(context.Context, string) error                     { return nil }
func (fakeService) DeleteElement(context.Context, string, string) error        { return nil }
func (fakeService) LogTail(context.Context) ([]string, error)                  { return nil, nil }
func (fakeService) Diagnostics(context.Context) []service.Check                { return nil }
func (fakeService) Sessions(context.Context) ([]accel.Session, error)          { return nil, nil }
func (fakeService) TerminateSession(context.Context, string) (string, error) {
	return "", nil
}
func (fakeService) ReloadAccel(context.Context) (string, error)      { return "", nil }
func (fakeService) RestartAccel(context.Context) (string, error)     { return "", nil }
func (fakeService) HardRestartAccel(context.Context) (string, error) { return "", nil }

func TestNewBuildsAllPages(t *testing.T) {
	app := New(fakeService{}, config.Defaults())
	for _, name := range []string{pageDashboard, pageSubscribers, pageBlocks, pageLogs, pageSessions, pageDiag} {
		if !app.pages.HasPage(name) {
			t.Fatalf("нет страницы %s", name)
		}
	}
	if app.pageName() != pageDashboard {
		t.Fatalf("стартовая страница %q", app.pageName())
	}
	if app.rings[pageSubscribers] == nil || len(app.rings[pageSubscribers].items) != 7 {
		t.Fatalf("фокус-кольцо абонентов: %+v", app.rings[pageSubscribers])
	}
	if app.rings[pageBlocks] == nil || len(app.rings[pageBlocks].items) != 6 {
		t.Fatalf("фокус-кольцо nft: %+v", app.rings[pageBlocks])
	}
	if app.rings[pageSessions] == nil || len(app.rings[pageSessions].items) != 5 {
		t.Fatalf("фокус-кольцо сессий: %+v", app.rings[pageSessions])
	}
	if app.rings[pageLogs] == nil || len(app.rings[pageLogs].items) != 2 {
		t.Fatalf("фокус-кольцо логов: %+v", app.rings[pageLogs])
	}
}

func TestFocusRingIndexOf(t *testing.T) {
	a := tview.NewBox()
	b := tview.NewBox()
	ring := newFocusRing(a, b)
	if ring.indexOf(a) != 0 || ring.indexOf(b) != 1 {
		t.Fatal("неверные индексы")
	}
	if ring.indexOf(tview.NewBox()) != -1 {
		t.Fatal("посторонний элемент должен давать -1")
	}
}

func TestHelpText(t *testing.T) {
	for _, page := range []string{pageDashboard, pageSubscribers, pageBlocks, pageLogs, pageSessions, pageDiag} {
		if txt := helpText(page); txt == "" {
			t.Fatalf("пустая подсказка для %s", page)
		}
	}
	if !strings.Contains(helpText(pageDashboard), "F1") {
		t.Fatal("в подсказке нет F1")
	}
}

func TestFormatHelpers(t *testing.T) {
	cases := map[float64]string{
		512:             "512 B",
		1536:            "1.5 KB",
		1024 * 1024:     "1.0 MB",
		3 * 1024 * 1024: "3.0 MB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Fatalf("%v -> %q, want %q", in, got, want)
		}
	}
	if maskValue("Cleartext-Password", "secret") != "********" {
		t.Fatal("пароль не замаскирован")
	}
	if maskValue("Framed-IP-Address", "192.0.2.2") != "192.0.2.2" {
		t.Fatal("IP не должен маскироваться")
	}
	if colorizeLogLine("Access-Reject") != "[red]Access-Reject[-]" {
		t.Fatal("Access-Reject не подсвечен")
	}
	if !strings.HasPrefix(colorizeLogLine("warn: authentication failed"), "[red]") {
		t.Fatal("authentication failed должен быть красным")
	}
}

func TestScrollbarThumb(t *testing.T) {
	if pos, size := scrollbarThumb(100, 10, 0, 20); pos != 0 || size != 2 {
		t.Fatalf("top: pos=%d size=%d", pos, size)
	}
	if pos, size := scrollbarThumb(100, 10, 90, 20); pos != 18 || size != 2 {
		t.Fatalf("bottom: pos=%d size=%d", pos, size)
	}
	if pos, size := scrollbarThumb(5, 10, 0, 20); pos != 0 || size != 20 {
		t.Fatalf("short text: pos=%d size=%d", pos, size)
	}
	if pos, size := scrollbarThumb(0, 10, 0, 20); pos != 0 || size != 0 {
		t.Fatalf("empty: pos=%d size=%d", pos, size)
	}
}

func TestHighlightLogLine(t *testing.T) {
	got := highlightLogLine("Access-Reject for user", "user")
	if !strings.HasPrefix(got, "[red]") {
		t.Fatalf("потерян базовый цвет: %q", got)
	}
	if !strings.Contains(got, "[yellow]user[-]") {
		t.Fatalf("нет подсветки: %q", got)
	}
}

func TestSessionsFilter(t *testing.T) {
	app := New(fakeService{}, config.Defaults())
	app.sess.SetSessions([]accel.Session{
		{IfName: "ppp0", Username: "u1", IP: "192.0.2.2", Type: "pppoe", State: "active"},
		{IfName: "eth0.100", Username: "02:00:00:00:00:01", IP: "192.0.2.2", Type: "ipoe", State: "active"},
	}, "")
	if len(app.sess.filtered) != 2 {
		t.Fatalf("len=%d", len(app.sess.filtered))
	}
	app.sess.query = "ipoe"
	app.sess.apply("", true)
	if len(app.sess.filtered) != 1 || app.sess.filtered[0].IfName != "eth0.100" {
		t.Fatalf("фильтр: %+v", app.sess.filtered)
	}
	if s, ok := app.sess.SelectedSession(); !ok || s.Username != "02:00:00:00:00:01" {
		t.Fatalf("selected: %+v ok=%v", s, ok)
	}
}

func TestSubscriberPasswordToggle(t *testing.T) {
	app := New(fakeService{}, config.Defaults())
	p := app.subs
	p.onSelect = nil
	p.SetRows([]store.SubscriberRow{{Username: "zz-ui-pw-1"}}, "")
	p.RenderDetails(store.Details{
		Username: "zz-ui-pw-1",
		Checks: []store.CheckRow{
			{Attribute: "Cleartext-Password", Value: "Secret123"},
		},
	}, nil)
	if txt := p.details.GetText(true); !strings.Contains(txt, "********") || strings.Contains(txt, "Secret123") {
		t.Fatalf("пароль не замаскирован: %q", txt)
	}
	p.TogglePassword()
	if txt := p.details.GetText(true); !strings.Contains(txt, "Secret123") {
		t.Fatalf("пароль не показан: %q", txt)
	}
	p.TogglePassword()
	if txt := p.details.GetText(true); strings.Contains(txt, "Secret123") {
		t.Fatalf("пароль не скрыт обратно: %q", txt)
	}
}

func TestSubscriberMatches(t *testing.T) {
	r := store.SubscriberRow{
		Username: "02:00:00:00:00:01",
		IP:       "192.0.2.2",
		IsIPoE:   true,
	}
	for _, q := range []string{"02:00", "02000000", "192.0.2", "0:01"} {
		if !subscriberMatches(r, q) {
			t.Fatalf("не найден по %q", q)
		}
	}
	for _, q := range []string{"demo_user_01", "192.0.2.99"} {
		if subscriberMatches(r, q) {
			t.Fatalf("ложное совпадение по %q", q)
		}
	}
}

func TestLogsFilter(t *testing.T) {
	p := newLogsPage()
	p.Render([]string{
		"[2026] info: ok",
		"[2026] warn: authentication failed",
		"[2026] info: Access-Reject",
	}, nil)
	if len(p.filtered) != 3 {
		t.Fatalf("len=%d", len(p.filtered))
	}
	p.query = "reject"
	p.apply(false)
	if len(p.filtered) != 1 {
		t.Fatalf("filtered=%+v", p.filtered)
	}
	if !strings.Contains(p.view.GetText(true), "Access-Reject") {
		t.Fatal("нет строки в TextView")
	}
}
