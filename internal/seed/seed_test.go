package seed

import (
	"context"
	"errors"
	"testing"

	"github.com/GokturkFK/gokturk-core/trap"
)

type fakeStore struct {
	decoys   int
	samples  []Sample
	countErr error
}

func (f *fakeStore) CountDecoys(context.Context) (int, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.decoys, nil
}

func (f *fakeStore) ProfileSamples(_ context.Context, limit int) ([]Sample, error) {
	if len(f.samples) > limit {
		return f.samples[:limit], nil
	}
	return f.samples, nil
}

type fakeProvisioner struct {
	profiles *Profiles
	calls    int
	seen     []Sample
	err      error
}

func (f *fakeProvisioner) Provision(context.Context, string) (*trap.Trap, *trap.Artifacts, error) {
	f.calls++
	if f.err != nil {
		return nil, nil, f.err
	}
	// Gercek Provider ne yapiyorsa: profil uretecini cagirir.
	t, o, s := f.profiles.Generate()
	f.seen = append(f.seen, Sample{NHIType: t, Owner: o, Scope: s})
	return &trap.Trap{ID: "decoy"}, &trap.Artifacts{}, nil
}

func newSeeder(store *fakeStore, target int) (*Seeder, *fakeProvisioner) {
	p := NewProfiles(func(int) (int, error) { return 0, nil })
	prov := &fakeProvisioner{profiles: p}
	return New(store, p, prov, target), prov
}

func TestEnsure_PlantsMissingDecoys(t *testing.T) {
	store := &fakeStore{decoys: 0, samples: []Sample{{"service_account", "billing", "read"}}}
	s, prov := newSeeder(store, 2)

	ids, err := s.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || prov.calls != 2 {
		t.Fatalf("ekilen = %d, provision = %d, istenen 2/2", len(ids), prov.calls)
	}
}

// Idempotanlik: her envanter turundan sonra cagrildigi icin, hedefe
// ulasilmissa YENI tuzak ekmemeli.
func TestEnsure_IdempotentWhenTargetMet(t *testing.T) {
	store := &fakeStore{decoys: 1, samples: []Sample{{"token", "ci", "write"}}}
	s, prov := newSeeder(store, 1)

	for i := 0; i < 3; i++ {
		ids, err := s.Ensure(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 0 {
			t.Fatalf("%d. cagri %d tuzak ekti, istenen 0", i+1, len(ids))
		}
	}
	if prov.calls != 0 {
		t.Errorf("provision cagrisi = %d, istenen 0", prov.calls)
	}
}

// Bos envantere ekim ERTELENIR: benzeyecegi kayit yokken ekilen tuzak
// tek basina durur ve ayirt edilebilir olur.
func TestEnsure_EmptyInventoryDefersSeeding(t *testing.T) {
	store := &fakeStore{decoys: 0}
	s, prov := newSeeder(store, 1)

	ids, err := s.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 || prov.calls != 0 {
		t.Fatalf("bos envanterde ekim yapildi: n=%d calls=%d", len(ids), prov.calls)
	}
}

func TestEnsure_DisabledWhenTargetZero(t *testing.T) {
	store := &fakeStore{samples: []Sample{{"token", "ci", "write"}}}
	s, prov := newSeeder(store, 0)

	if ids, err := s.Ensure(context.Background()); err != nil || len(ids) != 0 {
		t.Fatalf("n=%d err=%v, istenen 0/nil", len(ids), err)
	}
	if prov.calls != 0 {
		t.Errorf("ekim kapaliyken provision cagrildi")
	}
}

func TestEnsure_StoreErrorPropagates(t *testing.T) {
	store := &fakeStore{countErr: errors.New("db down")}
	s, _ := newSeeder(store, 1)

	if _, err := s.Ensure(context.Background()); err == nil {
		t.Fatal("store hatasi yukari tasinmaliydi")
	}
}

// Profil UYDURULMAZ: uretilen ucler envanterde gorulen bir ornekle
// birebir ayni olmali (alanlar farkli kayitlardan harmanlanmamali).
func TestGenerate_CopiesSampleVerbatim(t *testing.T) {
	samples := []Sample{
		{"service_account", "billing", "read"},
		{"machine_identity", "platform", "admin"},
	}
	p := NewProfiles(func(int) (int, error) { return 1, nil })
	p.Load(samples)

	nhiType, owner, scope := p.Generate()
	got := Sample{nhiType, owner, scope}
	if got != samples[1] {
		t.Errorf("uretilen = %+v, istenen %+v", got, samples[1])
	}
}

func TestGenerate_EmptyPoolReturnsEmpty(t *testing.T) {
	p := NewProfiles(nil)
	if nhiType, owner, scope := p.Generate(); nhiType != "" || owner != "" || scope != "" {
		t.Errorf("bos havuzdan profil uretildi: %q/%q/%q", nhiType, owner, scope)
	}
}

// pick hata verirse veya araligin disina duserse panik/dizin hatasi degil,
// deterministik bir geri donus olmali.
func TestGenerate_BadPickFallsBackToFirst(t *testing.T) {
	samples := []Sample{{"token", "ci", "write"}}
	p := NewProfiles(func(int) (int, error) { return 99, errors.New("rand patladi") })
	p.Load(samples)

	nhiType, owner, scope := p.Generate()
	if (Sample{nhiType, owner, scope}) != samples[0] {
		t.Errorf("geri donus = %q/%q/%q", nhiType, owner, scope)
	}
}

func TestCryptoPick_InRange(t *testing.T) {
	for i := 0; i < 50; i++ {
		n, err := cryptoPick(5)
		if err != nil {
			t.Fatal(err)
		}
		if n < 0 || n >= 5 {
			t.Fatalf("secim = %d, aralik disinda", n)
		}
	}
	if n, err := cryptoPick(0); err != nil || n != 0 {
		t.Errorf("bos aralik = %d/%v", n, err)
	}
}
