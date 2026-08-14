package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokturk-core/trap"
	"github.com/GokturkFK/gokzincir/internal/blastradius"
	"github.com/GokturkFK/gokzincir/internal/inventory"
	"github.com/GokturkFK/gokzincir/internal/nhitrap"
	_ "github.com/lib/pq"
)

// Bu paketin testleri GERCEK Postgres'e karsi kosar. Sebep: GOKKALKAN'da
// sahte store'la gorunmeyip gercek DB'de cikan hatalar yasandi (uuid tipi,
// FK kisiti, timestamptz yuvarlamasi). Kalicilik katmani ancak gercek
// DB'ye karsi dogrulanabilir.

func setupStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://gokzincir:gokzincir@localhost:5434/gokzincir?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		skipOrFail(t, "postgres surucusu acilamadi: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		skipOrFail(t, "postgres erisilemez: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Semayi KOMPLE sifirla. Elle bakilan bir tablo listesi her yeni
	// migration'da sessizce eskir (GOKKALKAN'da tam bunu yasadik).
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("sema sifirlanamadi: %v", err)
	}
	applySchema(t, db)
	return New(db)
}

// skipOrFail, yerelde (DB yokken) testi atlar ama CI'da PATLATIR.
// Sessizce atlanan bir kalicilik testi CI'yi yaniltir.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("CI'da DB zorunlu — "+format, args...)
	}
	t.Skipf(format+" (yerelde atlaniyor)", args...)
}

func applySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE trip_events (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			event_id text NOT NULL UNIQUE,
			trap_id text,
			sensor text NOT NULL,
			source text NOT NULL,
			observed_at timestamptz NOT NULL,
			raw jsonb NOT NULL DEFAULT '{}'::jsonb,
			alert_id uuid,
			created_at timestamptz NOT NULL DEFAULT now())`,
		`CREATE TABLE alerts (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			severity text NOT NULL CHECK (severity IN ('High','Critical')),
			technique text,
			source text NOT NULL,
			status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','ack','closed')),
			first_seen timestamptz NOT NULL,
			last_seen timestamptz NOT NULL,
			trip_count integer NOT NULL DEFAULT 1,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now())`,
		`ALTER TABLE trip_events ADD CONSTRAINT fk_trip_events_alert
			FOREIGN KEY (alert_id) REFERENCES alerts (id)`,
		`CREATE TABLE nhi_identities (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			type text NOT NULL,
			owner text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			last_used_at timestamptz,
			scope text NOT NULL DEFAULT '',
			secret_ref_hash text,
			is_decoy boolean NOT NULL DEFAULT false)`,
		`CREATE TABLE nhi_edges (
			from_id uuid NOT NULL REFERENCES nhi_identities (id) ON DELETE CASCADE,
			to_id uuid NOT NULL REFERENCES nhi_identities (id) ON DELETE CASCADE,
			relation text NOT NULL DEFAULT 'access',
			PRIMARY KEY (from_id, to_id))`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("sema uygulanamadi (%s): %v", s[:40], err)
		}
	}
}

func uuidN(n int) string {
	// Deterministik, gecerli uuid'ler: "...-000000000001" gibi.
	const base = "00000000-0000-4000-8000-"
	return base + padded(n)
}

func padded(n int) string {
	s := "000000000000"
	d := []byte(s)
	i := len(d) - 1
	for n > 0 && i >= 0 {
		d[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(d)
}

func mustUpsert(t *testing.T, s *Store, id, typ, owner string) {
	t.Helper()
	if err := s.UpsertIdentity(context.Background(),
		inventory.Record{ID: id, NHIType: typ, Owner: owner}, time.Now().UTC()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

func TestUpsertIdentity_IsIdempotent(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	id := uuidN(1)

	first := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	if err := s.UpsertIdentity(ctx, inventory.Record{
		ID: id, NHIType: "service_account", Owner: "billing", Scope: "read",
	}, first); err != nil {
		t.Fatal(err)
	}
	// Ayni id, degismis alanlarla, DAHA SONRAKI bir toplama turunda.
	later := first.Add(48 * time.Hour)
	if err := s.UpsertIdentity(ctx, inventory.Record{
		ID: id, NHIType: "service_account", Owner: "billing-team", Scope: "read:write",
	}, later); err != nil {
		t.Fatal(err)
	}

	views, err := s.Identities(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("cift kayit olustu: %d satir", len(views))
	}
	if views[0].Owner != "billing-team" || views[0].Scope != "read:write" {
		t.Errorf("guncelleme uygulanmadi: %+v", views[0])
	}
	// created_at ILK gorulme aninda kalmali.
	if !views[0].CreatedAt.Equal(first) {
		t.Errorf("created_at = %v, ilk gorulme (%v) korunmaliydi", views[0].CreatedAt, first)
	}
}

// Envanter hatti ekilmis bir tuzagi "gercek"e cevirmemeli — aksi halde
// tuzak sessizce etkisiz kalir (Decoder yalnizca is_decoy=true tetikler).
func TestUpsertIdentity_DoesNotUnmarkDecoy(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	id := uuidN(2)

	if err := s.Create(ctx, nhitrap.Identity{
		ID: id, NHIType: "token", Owner: "soc", CreatedAt: time.Now().UTC(), IsDecoy: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Envanter ayni id'yi gercek kaynaktan toplamis gibi davran.
	if err := s.UpsertIdentity(ctx, inventory.Record{
		ID: id, NHIType: "token", Owner: "soc", Scope: "x",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := s.FindByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsDecoy {
		t.Error("envanter turu tuzagi 'gercek'e cevirdi — tuzak etkisiz kalirdi")
	}
}

func TestIdentities_ExcludesDecoys(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()

	mustUpsert(t, s, uuidN(10), "service_account", "real-team")
	if err := s.Create(ctx, nhitrap.Identity{
		ID: uuidN(11), NHIType: "service_account", Owner: "decoy-team",
		CreatedAt: time.Now().UTC(), IsDecoy: true,
	}); err != nil {
		t.Fatal(err)
	}

	views, err := s.Identities(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("envanterde %d satir, istenen 1 (tuzak sizmamali)", len(views))
	}
	if views[0].Owner != "real-team" {
		t.Errorf("donen kayit tuzak: %+v", views[0])
	}
}

func TestFindByID_UnknownReturnsNotFound(t *testing.T) {
	s := setupStore(t)
	_, err := s.FindByID(context.Background(), uuidN(999))
	if !errors.Is(err, nhitrap.ErrIdentityNotFound) {
		t.Fatalf("ErrIdentityNotFound bekleniyordu, geldi: %v", err)
	}
}

// blastradius.Graph sozlesmesinin GERCEK Postgres implementasyonu:
// a -> b -> c zinciri, ve tuzak dugum grafta sayilmamali.
func TestNeighborsAndTotalNodes_FeedBlastRadius(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()

	a, b, c := uuidN(21), uuidN(22), uuidN(23)
	decoy := uuidN(24)
	mustUpsert(t, s, a, "service_account", "team-a")
	mustUpsert(t, s, b, "service_account", "team-b")
	mustUpsert(t, s, c, "service_account", "team-c")
	if err := s.Create(ctx, nhitrap.Identity{
		ID: decoy, NHIType: "token", Owner: "soc", CreatedAt: time.Now().UTC(), IsDecoy: true,
	}); err != nil {
		t.Fatal(err)
	}

	for _, e := range [][2]string{{a, b}, {b, c}, {a, decoy}} {
		if err := s.UpsertEdge(ctx, e[0], e[1], ""); err != nil {
			t.Fatal(err)
		}
	}

	total, err := s.TotalNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("TotalNodes = %d, istenen 3 (tuzak sayilmamali)", total)
	}

	res, err := blastradius.Compute(ctx, s, a)
	if err != nil {
		t.Fatal(err)
	}
	got := res.ReachableSorted()
	if len(got) != 2 {
		t.Fatalf("ulasilan = %v, istenen b ve c (tuzak haric)", got)
	}
	if res.Score <= 0 || res.Score > 1 {
		t.Errorf("skor araligi disi: %v", res.Score)
	}
}

func TestUpsertEdge_IsIdempotent(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	a, b := uuidN(31), uuidN(32)
	mustUpsert(t, s, a, "token", "x")
	mustUpsert(t, s, b, "token", "y")

	for i := 0; i < 3; i++ {
		if err := s.UpsertEdge(ctx, a, b, "access"); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Neighbors(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 1 {
		t.Errorf("komsu sayisi = %d, istenen 1 (cift kenar olusmamali)", len(n))
	}
}

func TestInsertTripEvent_AndTripsBySource(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	ev := trap.TripEvent{
		EventID: "evt-1", TrapID: uuidN(41), Sensor: "nhi-access-log",
		Source: "attacker", ObservedAt: now, Raw: []byte(`{"k":"v"}`),
	}
	if err := s.InsertTripEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	// Ayni event_id tekrar: cift kayit olmamali (at-least-once teslimat).
	if err := s.InsertTripEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}

	trips, err := s.TripsBySource(ctx, "attacker", time.Hour, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("trip sayisi = %d, istenen 1", len(trips))
	}
	if trips[0].TrapID != uuidN(41) || trips[0].Sensor != "nhi-access-log" {
		t.Errorf("trip = %+v", trips[0])
	}
}

// Penceresi disinda kalan trip'ler donmemeli.
func TestTripsBySource_RespectsWindow(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	old := trap.TripEvent{
		EventID: "evt-old", Sensor: "s", Source: "attacker",
		ObservedAt: now.Add(-2 * time.Hour), Raw: []byte(`{}`),
	}
	if err := s.InsertTripEvent(ctx, old); err != nil {
		t.Fatal(err)
	}

	trips, err := s.TripsBySource(ctx, "attacker", 30*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 0 {
		t.Errorf("pencere disi trip dondu: %+v", trips)
	}
}

// Kampanya birlesmesi: ayni kaynak icin ikinci alarm YENI satir acmamali.
func TestUpsertAlert_MergesPerSource(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first := correlate.Alert{
		Severity: correlate.SeverityHigh, Technique: "T1078.004", Source: "attacker",
		FirstSeen: now, LastSeen: now, TripCount: 1,
	}
	id1, err := s.UpsertAlert(ctx, first)
	if err != nil {
		t.Fatal(err)
	}

	second := first
	second.Severity = correlate.SeverityCritical
	second.LastSeen = now.Add(time.Minute)
	second.TripCount = 2
	id2, err := s.UpsertAlert(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("ayni kaynak icin yeni alarm satiri acildi: %s != %s", id1, id2)
	}

	alerts, err := s.Alerts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alarm sayisi = %d, istenen 1", len(alerts))
	}
	if alerts[0].Severity != correlate.SeverityCritical || alerts[0].TripCount != 2 {
		t.Errorf("alarm = %+v", alerts[0])
	}
	if alerts[0].Technique != "T1078.004" {
		t.Errorf("technique = %q", alerts[0].Technique)
	}
}

func TestAlerts_EmptyReturnsEmptySlice(t *testing.T) {
	s := setupStore(t)
	alerts, err := s.Alerts(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if alerts == nil {
		t.Error("bos sonuc nil degil bos slice donmeli (JSON'da [] olmali)")
	}
}

// blastradius.Graph arayuzunu gercekten uyguladigimizi derleme zamaninda
// dogrular.
var _ blastradius.Graph = (*Store)(nil)

// nhitrap.Store ve inventory.Store arayuzleri de ayni sekilde.
var (
	_ nhitrap.Store   = (*Store)(nil)
	_ inventory.Store = (*Store)(nil)
)
