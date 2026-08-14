package nhitrap

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/GokturkFK/gokturk-core/trap"
)

type fakeStore struct {
	identities map[string]Identity
}

func newFakeStore() *fakeStore {
	return &fakeStore{identities: make(map[string]Identity)}
}

func (s *fakeStore) Create(_ context.Context, id Identity) error {
	s.identities[id.ID] = id
	return nil
}

func (s *fakeStore) FindByID(_ context.Context, nhiID string) (*Identity, error) {
	id, ok := s.identities[nhiID]
	if !ok {
		return nil, ErrIdentityNotFound
	}
	return &id, nil
}

type fakeProfiles struct {
	nhiType, owner, scope string
}

func (f fakeProfiles) Generate() (string, string, string) {
	return f.nhiType, f.owner, f.scope
}

func fixedTime(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func fixedID(id string) func() string {
	return func() string { return id }
}

func TestProvider_Provision(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfiles{nhiType: "service_account", owner: "billing-team", scope: "read:invoices"}
	when := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)

	p := NewProvider(store, profiles, fixedID("nhi-1"), fixedTime(when))

	tr, artifacts, err := p.Provision(context.Background(), "soc")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if tr.ID != "nhi-1" || tr.Type != TypeDecoyNHI || tr.Username != "billing-team" {
		t.Errorf("trap alanlari beklenmedik: %+v", tr)
	}
	if !tr.CreatedAt.Equal(when) {
		t.Errorf("CreatedAt = %v, istenen %v", tr.CreatedAt, when)
	}
	if artifacts == nil {
		t.Fatal("artifacts nil olmamali (bos struct donmeli)")
	}

	stored, err := store.FindByID(context.Background(), "nhi-1")
	if err != nil {
		t.Fatalf("store'a yazilmamis: %v", err)
	}
	if stored.Owner != "billing-team" || stored.NHIType != "service_account" {
		t.Errorf("stored = %+v", stored)
	}
}

func TestProvider_Provision_NoSecretLeak(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfiles{nhiType: "token", owner: "x", scope: "y"}
	p := NewProvider(store, profiles, fixedID("nhi-2"), fixedTime(time.Now()))

	_, artifacts, err := p.Provision(context.Background(), "creator")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if artifacts.Secret != "" || artifacts.Username != "" {
		t.Errorf("decoy NHI icin artifacts bos olmali, geldi: %+v", artifacts)
	}
}

func TestProvider_Provision_NilIDFn(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfiles{}
	p := NewProvider(store, profiles, nil, nil)

	if _, _, err := p.Provision(context.Background(), "x"); err == nil {
		t.Fatal("nil idFn icin hata bekleniyordu")
	}
}

func TestDecoder_Decode_TripDetected(t *testing.T) {
	store := newFakeStore()
	_ = store.Create(context.Background(), Identity{ID: "nhi-3", Owner: "billing-team", CreatedAt: time.Now()})

	d := NewDecoder(store, fixedID("evt-1"))

	usage := Usage{NHIID: "nhi-3", AccessedBy: "attacker-agent", ObservedAt: time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC)}
	line, _ := json.Marshal(usage)

	obs := trap.RawObservation{Sensor: "nhi-access-log", Line: string(line), ObservedAt: usage.ObservedAt}

	ev, err := d.Decode(obs)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if ev.EventID != "evt-1" || ev.TrapID != "nhi-3" || ev.Source != "attacker-agent" || ev.Sensor != "nhi-access-log" {
		t.Errorf("TripEvent alanlari beklenmedik: %+v", ev)
	}
}

// Meşru bir NHI kullanımı (envanterde karşılığı olmayan bir ID) hiçbir
// event üretmemeli — sıfır-FP tezinin kod düzeyindeki karşılığı (issue #3 AC).
func TestDecoder_Decode_LegitimateUsageProducesNoEvent(t *testing.T) {
	store := newFakeStore()
	d := NewDecoder(store, fixedID("evt-2"))

	usage := Usage{NHIID: "real-service-account-42", AccessedBy: "legit-service", ObservedAt: time.Now()}
	line, _ := json.Marshal(usage)
	obs := trap.RawObservation{Sensor: "nhi-access-log", Line: string(line), ObservedAt: usage.ObservedAt}

	ev, err := d.Decode(obs)
	if !errors.Is(err, trap.ErrNotATrip) {
		t.Fatalf("ErrNotATrip bekleniyordu, geldi: %v", err)
	}
	if ev != nil {
		t.Errorf("event nil olmali, geldi: %+v", ev)
	}
}

func TestDecoder_Decode_InvalidObservation(t *testing.T) {
	store := newFakeStore()
	d := NewDecoder(store, fixedID("evt-3"))

	obs := trap.RawObservation{Sensor: "nhi-access-log", Line: "not-json", ObservedAt: time.Now()}
	if _, err := d.Decode(obs); err == nil {
		t.Fatal("gecersiz gozlem icin hata bekleniyordu")
	}
}

func TestDecoder_Decode_MissingNHIID(t *testing.T) {
	store := newFakeStore()
	d := NewDecoder(store, fixedID("evt-4"))

	usage := Usage{AccessedBy: "x"}
	line, _ := json.Marshal(usage)
	obs := trap.RawObservation{Sensor: "nhi-access-log", Line: string(line), ObservedAt: time.Now()}

	if _, err := d.Decode(obs); err == nil {
		t.Fatal("bos nhi_id icin hata bekleniyordu")
	}
}

func TestDecoder_Decode_NilIDFn(t *testing.T) {
	store := newFakeStore()
	d := NewDecoder(store, nil)

	usage := Usage{NHIID: "x", AccessedBy: "y"}
	line, _ := json.Marshal(usage)
	obs := trap.RawObservation{Sensor: "nhi-access-log", Line: string(line), ObservedAt: time.Now()}

	if _, err := d.Decode(obs); err == nil {
		t.Fatal("nil idFn icin hata bekleniyordu")
	}
}

// trap.Provider ve trap.Decoder arayuzlerini gercekten uyguladigimizi
// derleme zamaninda dogrular.
var (
	_ trap.Provider = (*Provider)(nil)
	_ trap.Decoder  = (*Decoder)(nil)
)
