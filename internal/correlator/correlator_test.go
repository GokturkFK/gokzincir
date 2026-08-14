package correlator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokturk-core/trap"
)

type fakeStore struct {
	trips   []trap.TripEvent
	alerts  map[string]correlate.Alert
	nextID  int
	failIns bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{alerts: make(map[string]correlate.Alert)}
}

func (s *fakeStore) InsertTripEvent(_ context.Context, ev trap.TripEvent) error {
	if s.failIns {
		return errors.New("insert patladi")
	}
	for _, existing := range s.trips {
		if existing.EventID == ev.EventID {
			return nil // ON CONFLICT DO NOTHING taklidi
		}
	}
	s.trips = append(s.trips, ev)
	return nil
}

func (s *fakeStore) TripsBySource(_ context.Context, source string, window time.Duration, now time.Time) ([]trap.TripEvent, error) {
	var out []trap.TripEvent
	for _, t := range s.trips {
		if t.Source == source && !t.ObservedAt.Before(now.Add(-window)) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *fakeStore) UpsertAlert(_ context.Context, a correlate.Alert) (string, error) {
	// Kaynak basina TEK alarm (kampanya birlesmesi).
	for id, existing := range s.alerts {
		if existing.Source == a.Source {
			s.alerts[id] = a
			return id, nil
		}
	}
	s.nextID++
	id := "alert-" + string(rune('0'+s.nextID))
	s.alerts[id] = a
	return id, nil
}

func tripAt(id, source string, at time.Time) trap.TripEvent {
	return trap.TripEvent{
		EventID: id, TrapID: "decoy-1", Sensor: "nhi-access-log",
		Source: source, ObservedAt: at, Raw: []byte(`{}`),
	}
}

func TestHandle_FirstTripProducesHigh(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	e := New(store, WithClock(func() time.Time { return now }))

	res, err := e.Handle(context.Background(), tripAt("e1", "attacker", now), "T1078.004")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if len(res.Alerts) != 1 {
		t.Fatalf("alarm sayisi = %d, istenen 1", len(res.Alerts))
	}
	if res.Alerts[0].Severity != correlate.SeverityHigh {
		t.Errorf("severity = %q, istenen High", res.Alerts[0].Severity)
	}
	if res.Alerts[0].Technique != "T1078.004" {
		t.Errorf("technique = %q", res.Alerts[0].Technique)
	}
	if res.Alerts[0].ID == "" {
		t.Error("alarm id'si upsert'ten geri yazilmali")
	}
}

// Ayni kaynaktan ikinci trip TEK Critical alarma birlesmeli.
func TestHandle_SecondTripEscalatesToSingleCritical(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	e := New(store, WithClock(func() time.Time { return now }))
	ctx := context.Background()

	if _, err := e.Handle(ctx, tripAt("e1", "attacker", now.Add(-time.Minute)), "T1078.004"); err != nil {
		t.Fatal(err)
	}
	res, err := e.Handle(ctx, tripAt("e2", "attacker", now), "T1078.004")
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Alerts) != 1 {
		t.Fatalf("alarm sayisi = %d, istenen 1 (kampanya birlesmesi)", len(res.Alerts))
	}
	if res.Alerts[0].Severity != correlate.SeverityCritical {
		t.Errorf("severity = %q, istenen Critical", res.Alerts[0].Severity)
	}
	if res.Alerts[0].TripCount != 2 {
		t.Errorf("trip_count = %d, istenen 2", res.Alerts[0].TripCount)
	}
	if len(store.alerts) != 1 {
		t.Errorf("store'da %d alarm satiri var, istenen 1", len(store.alerts))
	}
}

// Farkli kaynaklar birlestirilmemeli.
func TestHandle_DifferentSourcesStaySeparate(t *testing.T) {
	store := newFakeStore()
	now := time.Now().UTC()
	e := New(store, WithClock(func() time.Time { return now }))
	ctx := context.Background()

	if _, err := e.Handle(ctx, tripAt("e1", "attacker-a", now), "T1078.004"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Handle(ctx, tripAt("e2", "attacker-b", now), "T1078.004"); err != nil {
		t.Fatal(err)
	}
	if len(store.alerts) != 2 {
		t.Errorf("alarm satiri = %d, istenen 2 (farkli kaynaklar)", len(store.alerts))
	}
}

// Pencere disindaki eski trip'ler kampanyaya sayilmamali.
func TestHandle_OldTripOutsideWindowNotCounted(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	e := New(store, WithClock(func() time.Time { return now }), WithWindow(10*time.Minute))
	ctx := context.Background()

	// Pencere disinda (1 saat once):
	if _, err := e.Handle(ctx, tripAt("e-old", "attacker", now.Add(-time.Hour)), "T1078.004"); err != nil {
		t.Fatal(err)
	}
	res, err := e.Handle(ctx, tripAt("e-new", "attacker", now), "T1078.004")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Alerts) != 1 {
		t.Fatalf("alarm sayisi = %d", len(res.Alerts))
	}
	if res.Alerts[0].Severity != correlate.SeverityHigh {
		t.Errorf("severity = %q, istenen High (eski trip sayilmamali)", res.Alerts[0].Severity)
	}
}

func TestHandle_EmptySourceRejected(t *testing.T) {
	e := New(newFakeStore())
	ev := tripAt("e1", "", time.Now())
	if _, err := e.Handle(context.Background(), ev, "T1078.004"); !errors.Is(err, ErrNoSource) {
		t.Fatalf("ErrNoSource bekleniyordu, geldi: %v", err)
	}
}

func TestHandle_StoreErrorPropagates(t *testing.T) {
	store := newFakeStore()
	store.failIns = true
	e := New(store)
	if _, err := e.Handle(context.Background(), tripAt("e1", "a", time.Now()), "T1078.004"); err == nil {
		t.Fatal("store hatasi yukari tasinmaliydi")
	}
}
