// Package inventory, GZ0-2: NHI kayıtlarını kanonik modele dönüştürüp
// kalıcı yazan hattır.
//
// v0.1 kapsamı bilinçli olarak tek ve taşınabilir bir kaynak tipi:
// JSON kayıt akışı (PROJECT_PLAN.md böl. 2 — gerçek bulut IAM
// entegrasyonu v0.2'ye ertelendi). Amaç graph/blast-radius mantığını
// gerçek veriyle doğrulamak, sağlayıcı SDK'sı yazmak değil.
//
// Hat İDEMPOTENT'tir: aynı kaynak iki kez toplanırsa çift kayıt oluşmaz
// (issue #5 AC). Bu, kalıcılık katmanındaki ON CONFLICT ile değil, burada
// da açıkça test edilir — envanter periyodik çalışacak bir iştir.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Record, kaynaktan gelen tek bir NHI kaydının kanonik halidir
// (docs/DECISIONS.md Karar 1).
//
// SecretRefHash bilinçli olarak "hash": sırrın kendisi asla taşınmaz ve
// yazılmaz — GÖKTÜRK'teki traps.secret_hash deseni.
type Record struct {
	ID            string     `json:"id"`
	NHIType       string     `json:"type"`
	Owner         string     `json:"owner"`
	Scope         string     `json:"scope"`
	SecretRefHash string     `json:"secret_ref_hash,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

// Edge, iki NHI arasındaki yönlü erişim ilişkisidir: From, To'ya
// erişebilir (docs/DECISIONS.md Karar 2).
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation,omitempty"`
}

// Snapshot, bir toplama turunun tamamıdır.
type Snapshot struct {
	Identities []Record `json:"identities"`
	Edges      []Edge   `json:"edges"`
}

// Store, envanterin yazma ihtiyacını soyutlar. Postgres implementasyonu
// internal/store'da.
type Store interface {
	UpsertIdentity(ctx context.Context, r Record, seenAt time.Time) error
	UpsertEdge(ctx context.Context, fromID, toID, relation string) error
}

// Hata değerleri: kaynak verisi bozuksa hat SESSİZCE kısmi yazmaz.
var (
	// ErrMissingID, bir kayıtta id alanı boşsa döner.
	ErrMissingID = errors.New("inventory: kayit id'si bos")
	// ErrMissingType, bir kayıtta type alanı boşsa döner.
	ErrMissingType = errors.New("inventory: kayit tipi bos")
	// ErrMissingOwner, bir kayıtta owner alanı boşsa döner.
	ErrMissingOwner = errors.New("inventory: kayit sahibi bos")
	// ErrEdgeEndpoint, bir kenarın uçlarından biri boşsa döner.
	ErrEdgeEndpoint = errors.New("inventory: kenar ucu bos")
)

// Ingester, bir Snapshot'ı doğrulayıp Store'a yazar.
type Ingester struct {
	store Store
	now   func() time.Time
}

// NewIngester, verilen Store ile bir Ingester kurar. now nil bırakılırsa
// time.Now kullanılır.
func NewIngester(store Store, now func() time.Time) *Ingester {
	if now == nil {
		now = time.Now
	}
	return &Ingester{store: store, now: now}
}

// Result, bir toplama turunun sonucudur.
type Result struct {
	Identities int
	Edges      int
}

// Ingest, snapshot'ı yazar.
//
// ÖNCE TÜM KAYITLAR DOĞRULANIR, sonra yazılır: bozuk tek bir kayıt
// yüzünden envanterin yarısının yazılıp yarısının yazılmaması, bir
// sonraki turda hangi kaydın güncel olduğunu belirsiz hale getirirdi.
// Kenarlar kimliklerden SONRA yazılır — nhi_edges'in FK'si kimliklerin
// önce var olmasını gerektirir.
func (in *Ingester) Ingest(ctx context.Context, snap Snapshot) (Result, error) {
	for i, r := range snap.Identities {
		if err := validateRecord(r); err != nil {
			return Result{}, fmt.Errorf("inventory: kayit %d gecersiz: %w", i, err)
		}
	}
	for i, e := range snap.Edges {
		if e.From == "" || e.To == "" {
			return Result{}, fmt.Errorf("inventory: kenar %d gecersiz: %w", i, ErrEdgeEndpoint)
		}
	}

	seenAt := in.now()
	for _, r := range snap.Identities {
		if err := in.store.UpsertIdentity(ctx, r, seenAt); err != nil {
			return Result{}, err
		}
	}
	for _, e := range snap.Edges {
		if err := in.store.UpsertEdge(ctx, e.From, e.To, e.Relation); err != nil {
			return Result{}, err
		}
	}
	return Result{Identities: len(snap.Identities), Edges: len(snap.Edges)}, nil
}

// IngestJSON, bir JSON akışını okuyup Ingest'e verir.
func (in *Ingester) IngestJSON(ctx context.Context, r io.Reader) (Result, error) {
	var snap Snapshot
	if err := json.NewDecoder(r).Decode(&snap); err != nil {
		return Result{}, fmt.Errorf("inventory: snapshot cozumlenemedi: %w", err)
	}
	return in.Ingest(ctx, snap)
}

func validateRecord(r Record) error {
	switch {
	case r.ID == "":
		return ErrMissingID
	case r.NHIType == "":
		return ErrMissingType
	case r.Owner == "":
		return ErrMissingOwner
	}
	return nil
}
