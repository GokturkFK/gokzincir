package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	identities map[string]Record
	seenAt     map[string]time.Time
	edges      map[[2]string]string
	failOn     string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		identities: make(map[string]Record),
		seenAt:     make(map[string]time.Time),
		edges:      make(map[[2]string]string),
	}
}

func (s *fakeStore) UpsertIdentity(_ context.Context, r Record, seenAt time.Time) error {
	if s.failOn == r.ID {
		return errors.New("store patladi")
	}
	s.identities[r.ID] = r
	s.seenAt[r.ID] = seenAt
	return nil
}

func (s *fakeStore) UpsertEdge(_ context.Context, from, to, rel string) error {
	if rel == "" {
		rel = "access"
	}
	s.edges[[2]string{from, to}] = rel
	return nil
}

func sample() Snapshot {
	return Snapshot{
		Identities: []Record{
			{ID: "sa-1", NHIType: "service_account", Owner: "billing", Scope: "read:invoices"},
			{ID: "sa-2", NHIType: "token", Owner: "ci", Scope: "deploy"},
		},
		Edges: []Edge{{From: "sa-1", To: "sa-2"}},
	}
}

func TestIngest_WritesIdentitiesAndEdges(t *testing.T) {
	store := newFakeStore()
	in := NewIngester(store, func() time.Time { return time.Unix(1000, 0).UTC() })

	res, err := in.Ingest(context.Background(), sample())
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if res.Identities != 2 || res.Edges != 1 {
		t.Errorf("res = %+v", res)
	}
	if len(store.identities) != 2 || len(store.edges) != 1 {
		t.Errorf("store = %+v / %+v", store.identities, store.edges)
	}
	if got := store.edges[[2]string{"sa-1", "sa-2"}]; got != "access" {
		t.Errorf("varsayilan relation = %q, istenen access", got)
	}
}

// Issue #5 AC: tekrar calistirma cift kayit uretmemeli.
func TestIngest_IsIdempotent(t *testing.T) {
	store := newFakeStore()
	in := NewIngester(store, nil)

	if _, err := in.Ingest(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Ingest(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}

	if len(store.identities) != 2 {
		t.Errorf("ikinci turdan sonra kimlik sayisi = %d, istenen 2", len(store.identities))
	}
	if len(store.edges) != 1 {
		t.Errorf("ikinci turdan sonra kenar sayisi = %d, istenen 1", len(store.edges))
	}
}

// Bozuk TEK bir kayit yuzunden envanterin yarisi yazilip yarisi
// yazilmamali: bir sonraki turda hangi kaydin guncel oldugu belirsizlesir.
func TestIngest_InvalidRecordWritesNothing(t *testing.T) {
	store := newFakeStore()
	in := NewIngester(store, nil)

	snap := sample()
	snap.Identities = append(snap.Identities, Record{ID: "", NHIType: "token", Owner: "x"})

	if _, err := in.Ingest(context.Background(), snap); !errors.Is(err, ErrMissingID) {
		t.Fatalf("ErrMissingID bekleniyordu, geldi: %v", err)
	}
	if len(store.identities) != 0 {
		t.Errorf("hicbir sey yazilmamaliydi, yazilan: %+v", store.identities)
	}
	if len(store.edges) != 0 {
		t.Errorf("hicbir kenar yazilmamaliydi, yazilan: %+v", store.edges)
	}
}

func TestIngest_MissingTypeAndOwnerRejected(t *testing.T) {
	in := NewIngester(newFakeStore(), nil)

	noType := Snapshot{Identities: []Record{{ID: "a", Owner: "o"}}}
	if _, err := in.Ingest(context.Background(), noType); !errors.Is(err, ErrMissingType) {
		t.Errorf("ErrMissingType bekleniyordu, geldi: %v", err)
	}

	noOwner := Snapshot{Identities: []Record{{ID: "a", NHIType: "token"}}}
	if _, err := in.Ingest(context.Background(), noOwner); !errors.Is(err, ErrMissingOwner) {
		t.Errorf("ErrMissingOwner bekleniyordu, geldi: %v", err)
	}
}

func TestIngest_EdgeWithEmptyEndpointRejected(t *testing.T) {
	store := newFakeStore()
	in := NewIngester(store, nil)

	snap := Snapshot{
		Identities: []Record{{ID: "a", NHIType: "token", Owner: "o"}},
		Edges:      []Edge{{From: "a", To: ""}},
	}
	if _, err := in.Ingest(context.Background(), snap); !errors.Is(err, ErrEdgeEndpoint) {
		t.Fatalf("ErrEdgeEndpoint bekleniyordu, geldi: %v", err)
	}
	if len(store.identities) != 0 {
		t.Error("kenar gecersizken kimlikler de yazilmamaliydi")
	}
}

func TestIngestJSON_ParsesSnapshot(t *testing.T) {
	store := newFakeStore()
	in := NewIngester(store, nil)

	body := `{
	  "identities": [
	    {"id":"sa-9","type":"service_account","owner":"data","scope":"read:s3","last_used_at":"2026-08-01T10:00:00Z"}
	  ],
	  "edges": [{"from":"sa-9","to":"sa-9","relation":"access"}]
	}`

	res, err := in.IngestJSON(context.Background(), strings.NewReader(body))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if res.Identities != 1 || res.Edges != 1 {
		t.Errorf("res = %+v", res)
	}
	got := store.identities["sa-9"]
	if got.Owner != "data" || got.Scope != "read:s3" {
		t.Errorf("kayit = %+v", got)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("last_used_at = %v", got.LastUsedAt)
	}
}

func TestIngestJSON_InvalidJSON(t *testing.T) {
	in := NewIngester(newFakeStore(), nil)
	if _, err := in.IngestJSON(context.Background(), strings.NewReader("{bozuk")); err == nil {
		t.Fatal("bozuk JSON icin hata bekleniyordu")
	}
}

func TestIngest_StoreErrorPropagates(t *testing.T) {
	store := newFakeStore()
	store.failOn = "sa-2"
	in := NewIngester(store, nil)

	if _, err := in.Ingest(context.Background(), sample()); err == nil {
		t.Fatal("store hatasi yukari tasinmaliydi")
	}
}
