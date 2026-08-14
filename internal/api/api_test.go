package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokzincir/internal/store"
)

type fakeStore struct {
	alerts     []correlate.Alert
	identities []store.IdentityView
	edges      map[string][]string
	total      int
}

func (f *fakeStore) Alerts(_ context.Context, limit int) ([]correlate.Alert, error) {
	if len(f.alerts) > limit {
		return f.alerts[:limit], nil
	}
	return f.alerts, nil
}

func (f *fakeStore) Identities(_ context.Context, limit int) ([]store.IdentityView, error) {
	if len(f.identities) > limit {
		return f.identities[:limit], nil
	}
	return f.identities, nil
}

func (f *fakeStore) Neighbors(_ context.Context, id string) ([]string, error) {
	return f.edges[id], nil
}

func (f *fakeStore) TotalNodes(_ context.Context) (int, error) { return f.total, nil }

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newServer(s Store) *httptest.Server {
	mux := http.NewServeMux()
	New(s, quiet()).Routes(mux)
	return httptest.NewServer(mux)
}

func TestAlerts_ReturnsCoreContract(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	fs := &fakeStore{alerts: []correlate.Alert{{
		ID: "a1", Severity: correlate.SeverityCritical, Technique: "T1078.004",
		Source: "attacker", Status: correlate.StatusOpen,
		FirstSeen: now, LastSeen: now, TripCount: 2,
	}}}
	srv := newServer(fs)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/alerts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Panel bu alanlari bekliyor (GOKTURK/GOKKALKAN ile ayni sozlesme).
	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("alarm sayisi = %d", len(got))
	}
	for _, k := range []string{"id", "severity", "technique", "source", "status", "first_seen", "last_seen", "trip_count"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("sozlesme alani eksik: %q", k)
		}
	}
	if got[0]["technique"] != "T1078.004" {
		t.Errorf("technique = %v", got[0]["technique"])
	}
}

// Bos sonuc null degil [] donmeli — panel null kontrolu yapmasin.
func TestAlerts_EmptyReturnsEmptyArray(t *testing.T) {
	srv := newServer(&fakeStore{alerts: []correlate.Alert{}})
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/alerts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]\n" {
		t.Errorf("bos sonuc = %q, istenen []", string(body))
	}
}

// Tuzaklar ve sir ozeti okuma ucundan ASLA cikmamali
// (docs/DECISIONS.md Karar 1/5).
func TestIdentities_DoesNotLeakDecoyOrSecret(t *testing.T) {
	fs := &fakeStore{identities: []store.IdentityView{{
		ID: "sa-1", NHIType: "service_account", Owner: "billing",
		Scope: "read", CreatedAt: time.Now().UTC(),
	}}}
	srv := newServer(fs)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/nhi")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	for _, forbidden := range []string{"is_decoy", "IsDecoy", "secret_ref_hash", "SecretRefHash"} {
		if contains(string(body), forbidden) {
			t.Errorf("okuma ucu %q alanini sizdirdi: %s", forbidden, string(body))
		}
	}
}

func TestBlastRadius_ComputesOverGraph(t *testing.T) {
	fs := &fakeStore{
		edges: map[string][]string{"a": {"b"}, "b": {"c"}},
		total: 4,
	}
	srv := newServer(fs)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/nhi/a/blast-radius")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got BlastRadiusResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SourceID != "a" || got.Count != 2 {
		t.Errorf("sonuc = %+v", got)
	}
	if got.Score != 0.5 {
		t.Errorf("skor = %v, istenen 0.5 (2/4)", got.Score)
	}
	if got.MaxDepth != 5 {
		t.Errorf("max_depth = %d", got.MaxDepth)
	}
}

func TestLimit_Invalid(t *testing.T) {
	srv := newServer(&fakeStore{})
	defer srv.Close()

	for _, bad := range []string{"0", "-1", "abc"} {
		resp, err := srv.Client().Get(srv.URL + "/api/v1/alerts?limit=" + bad)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%q icin status = %d, istenen 400", bad, resp.StatusCode)
		}
	}
}

func TestParseLimit_ClampsToMax(t *testing.T) {
	n, err := parseLimit("999999")
	if err != nil {
		t.Fatal(err)
	}
	if n != MaxLimit {
		t.Errorf("limit = %d, istenen %d", n, MaxLimit)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
