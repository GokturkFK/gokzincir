// Package store, GÖKZİNCİR'in Postgres erişim katmanıdır.
//
// Diğer paketler (nhitrap, blastradius, inventory, api) DB'yi doğrudan
// tanımaz; kendi dar arayüzlerini tanımlar ve bu paket onları karşılar.
// Bu ayrım GÖKKALKAN'da da böyleydi: güvenlik çekirdeği saf/test edilebilir
// kalır, kalıcılık kararları tek yerde toplanır.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokturk-core/trap"
	"github.com/GokturkFK/gokzincir/internal/inventory"
	"github.com/GokturkFK/gokzincir/internal/nhitrap"
	"github.com/GokturkFK/gokzincir/internal/seed"
)

// Store, Postgres üzerinde çalışan kalıcılık katmanıdır.
type Store struct {
	db *sql.DB
}

// New, verilen bağlantı havuzuyla bir Store kurar.
func New(db *sql.DB) *Store { return &Store{db: db} }

// --- nhi_identities ---

// Create, nhitrap.Store sözleşmesini karşılar: bir NHI kaydını yazar.
//
// is_decoy AÇIKÇA yazılır — kolonun DB varsayılanına (false) güvenmek,
// ekilen tuzağın sessizce gerçek kayıt gibi görünmesine yol açardı
// (bkz. internal/nhitrap, tuzak/gerçek ayrımı).
func (s *Store) Create(ctx context.Context, id nhitrap.Identity) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nhi_identities (id, type, owner, scope, secret_ref_hash, is_decoy, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (id) DO NOTHING`,
		id.ID, id.NHIType, id.Owner, id.Scope, nullString(id.SecretRefHash), id.IsDecoy, id.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: nhi yazilamadi: %w", err)
	}
	return nil
}

// FindByID, nhitrap.Store sözleşmesini karşılar.
//
// Tuzak filtresi BURADA YAPILMAZ: sorgu gerçek kayıtları da döner ve
// "tuzak mı" ayrımını çağıran (nhitrap.Decoder) IsDecoy alanına bakarak
// verir. Filtreyi buraya gömmek, adı "FindByID" olan bir fonksiyonun
// sessizce bazı kayıtları gizlemesi olurdu — ayrımın nerede yapıldığı
// tespit mantığının parçası, kalıcılık katmanının değil.
func (s *Store) FindByID(ctx context.Context, nhiID string) (*nhitrap.Identity, error) {
	var (
		id        nhitrap.Identity
		scope     sql.NullString
		secretRef sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id::text, type, owner, COALESCE(scope, ''), secret_ref_hash, is_decoy, created_at
		   FROM nhi_identities WHERE id = $1`, nhiID,
	).Scan(&id.ID, &id.NHIType, &id.Owner, &scope, &secretRef, &id.IsDecoy, &id.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nhitrap.ErrIdentityNotFound
	}
	if err != nil {
		// Gecersiz uuid metni de dahil: kayit yok muamelesi yapilir,
		// cunku cagiran icin ikisi de "bu bir tuzak degil" demektir.
		return nil, fmt.Errorf("store: nhi okunamadi: %w", err)
	}
	id.Scope = scope.String
	id.SecretRefHash = secretRef.String
	return &id, nil
}

// CountDecoys, seed.Store sözleşmesini karşılar: ekili tuzak sayısı.
// Ekimin idempotent olması buna dayanır.
func (s *Store) CountDecoys(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM nhi_identities WHERE is_decoy`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: tuzak sayisi okunamadi: %w", err)
	}
	return n, nil
}

// ProfileSamples, seed.Store sözleşmesini karşılar: ekilecek tuzağın
// benzeyeceği GERÇEK envanter profilleri.
//
// is_decoy = false şart: mevcut tuzaklardan örnek almak, ikinci tuzağı
// birincinin kopyası yapar ve zamanla envanterde birbirinin aynısı bir
// tuzak kümesi oluştururdu — tam olarak göze batması istenmeyen şey.
func (s *Store) ProfileSamples(ctx context.Context, limit int) ([]seed.Sample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT type, owner, COALESCE(scope, '')
		   FROM nhi_identities
		  WHERE NOT is_decoy
		  ORDER BY created_at DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: profil ornekleri okunamadi: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []seed.Sample
	for rows.Next() {
		var sm seed.Sample
		if err := rows.Scan(&sm.NHIType, &sm.Owner, &sm.Scope); err != nil {
			return nil, fmt.Errorf("store: profil ornegi cozulemedi: %w", err)
		}
		out = append(out, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: profil ornekleri okunamadi: %w", err)
	}
	return out, nil
}

// --- envanter (GZ0-2) ---

// UpsertIdentity, inventory.Store sözleşmesini karşılar: aynı NHI tekrar
// toplandığında yeni satır açmaz, değişen alanları günceller (idempotent).
//
// created_at KORUNUR (envanterin o kaydı ilk öğrendiği an; tekrar toplama
// onu ileri kaydırmamalı — GÖKKALKAN'daki revoked_at disiplininin aynısı),
// bu yüzden UPDATE listesinde yok.
//
// is_decoy de KORUNUR ve yalnızca INSERT yolunda false yazılır: envanter
// hattı gerçek kaynaklardan beslenir; ekilmiş bir tuzağın id'si kaynakta
// da görünüyorsa (tuzak zaten envanterin içinde durmak üzere tasarlandı)
// onu "gerçek"e çevirmek tuzağı sessizce etkisiz kılardı.
func (s *Store) UpsertIdentity(ctx context.Context, r inventory.Record, seenAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nhi_identities (id, type, owner, scope, secret_ref_hash, last_used_at, is_decoy, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, false, $7)
		 ON CONFLICT (id) DO UPDATE SET
		     type            = EXCLUDED.type,
		     owner           = EXCLUDED.owner,
		     scope           = EXCLUDED.scope,
		     secret_ref_hash = EXCLUDED.secret_ref_hash,
		     last_used_at    = EXCLUDED.last_used_at`,
		r.ID, r.NHIType, r.Owner, r.Scope, nullString(r.SecretRefHash),
		nullTimePtr(r.LastUsedAt), seenAt)
	if err != nil {
		return fmt.Errorf("store: nhi upsert edilemedi: %w", err)
	}
	return nil
}

// UpsertEdge, iki NHI arasındaki yönlü erişim kenarını yazar (idempotent).
func (s *Store) UpsertEdge(ctx context.Context, fromID, toID, relation string) error {
	if relation == "" {
		relation = "access"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nhi_edges (from_id, to_id, relation) VALUES ($1, $2, $3)
		 ON CONFLICT (from_id, to_id) DO UPDATE SET relation = EXCLUDED.relation`,
		fromID, toID, relation)
	if err != nil {
		return fmt.Errorf("store: kenar yazilamadi: %w", err)
	}
	return nil
}

// IdentityView, okuma uçlarına (GZ0-5) dönen NHI görünümüdür.
//
// is_decoy ve secret_ref_hash BİLİNÇLİ OLARAK YOK: tuzağın envanterde
// ayırt edilebilir olmaması ürünün tezinin parçası (docs/DECISIONS.md
// Karar 1/5) ve sır özeti hiçbir okuma ucundan dışarı çıkmaz. Ayrı bir
// tip kullanılmasının sebebi bu — nhitrap.Identity'yi doğrudan JSON'a
// vermek, ileride eklenecek bir alanın sessizce sızması demek olurdu.
type IdentityView struct {
	ID         string     `json:"id"`
	NHIType    string     `json:"type"`
	Owner      string     `json:"owner"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Identities, envanteri okur. Tuzaklar (is_decoy = true) DÖNMEZ.
func (s *Store) Identities(ctx context.Context, limit int) ([]IdentityView, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id::text, type, owner, COALESCE(scope, ''), created_at, last_used_at
		   FROM nhi_identities
		  WHERE is_decoy = false
		  ORDER BY created_at DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: envanter okunamadi: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]IdentityView, 0)
	for rows.Next() {
		var (
			v    IdentityView
			last sql.NullTime
		)
		if err := rows.Scan(&v.ID, &v.NHIType, &v.Owner, &v.Scope, &v.CreatedAt, &last); err != nil {
			return nil, fmt.Errorf("store: envanter satiri okunamadi: %w", err)
		}
		if last.Valid {
			t := last.Time
			v.LastUsedAt = &t
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: envanter iterasyonu: %w", err)
	}
	return out, nil
}

// --- graph (GZ0-3) ---

// Neighbors, blastradius.Graph sözleşmesini karşılar: verilen düğümden
// YÖNLÜ olarak ulaşılabilen komşuları döner (docs/DECISIONS.md Karar 2).
//
// Tuzaklar graf'a dahil EDİLMEZ: blast-radius gerçek hasar yüzeyini
// ölçer; kendi ektiğimiz bir tuzağı "ulaşılabilir varlık" saymak skoru
// yapay olarak şişirirdi.
func (s *Store) Neighbors(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.to_id::text
		   FROM nhi_edges e
		   JOIN nhi_identities n ON n.id = e.to_id
		  WHERE e.from_id = $1 AND n.is_decoy = false`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("store: komsular okunamadi: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: komsu satiri okunamadi: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: komsu iterasyonu: %w", err)
	}
	return out, nil
}

// TotalNodes, blastradius.Graph sözleşmesini karşılar: normalize skorun
// paydası. Neighbors ile tutarlı olması için tuzaklar burada da sayılmaz —
// aksi halde skor (ulaşılan/toplam) iki farklı evrene bölünürdü.
func (s *Store) TotalNodes(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM nhi_identities WHERE is_decoy = false`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: dugum sayisi okunamadi: %w", err)
	}
	return n, nil
}

// --- trip_events / alerts (GZ0-4) ---

// InsertTripEvent, bir trip'i kalıcı yazar. Aynı event_id tekrar gelirse
// yok sayılır (at-least-once teslimat altında çift kayıt olmasın).
func (s *Store) InsertTripEvent(ctx context.Context, ev trap.TripEvent) error {
	raw := ev.Raw
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO trip_events (event_id, trap_id, sensor, source, observed_at, raw)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (event_id) DO NOTHING`,
		ev.EventID, nullString(ev.TrapID), ev.Sensor, ev.Source, ev.ObservedAt, []byte(raw))
	if err != nil {
		return fmt.Errorf("store: trip event yazilamadi: %w", err)
	}
	return nil
}

// TripsBySource, verilen kaynak için son `window` süresindeki trip'leri
// döner. Korelasyon penceresini seçmek çağıranın işidir
// (gokturk-core/correlate paket sözleşmesi).
func (s *Store) TripsBySource(ctx context.Context, source string, window time.Duration, now time.Time) ([]trap.TripEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_id, COALESCE(trap_id, ''), sensor, source, observed_at, raw
		   FROM trip_events
		  WHERE source = $1 AND observed_at >= $2
		  ORDER BY observed_at`,
		source, now.Add(-window))
	if err != nil {
		return nil, fmt.Errorf("store: trip'ler okunamadi: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []trap.TripEvent
	for rows.Next() {
		var (
			ev  trap.TripEvent
			raw []byte
		)
		if err := rows.Scan(&ev.EventID, &ev.TrapID, &ev.Sensor, &ev.Source, &ev.ObservedAt, &raw); err != nil {
			return nil, fmt.Errorf("store: trip satiri okunamadi: %w", err)
		}
		ev.Raw = raw
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: trip iterasyonu: %w", err)
	}
	return out, nil
}

// UpsertAlert, bir kaynak için alarmı yazar veya günceller.
//
// Kampanya birleşmesi (aynı kaynaktan 2. trip → tek Critical) ancak alarm
// kaynak başına TEK satır kalırsa görünür; bu yüzden açık bir alarm varsa
// güncellenir, yeni satır açılmaz. GÖKKALKAN'daki desenin aynısı.
func (s *Store) UpsertAlert(ctx context.Context, a correlate.Alert) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`UPDATE alerts
		    SET severity = $1, technique = $2, last_seen = $3, trip_count = $4, updated_at = now()
		  WHERE source = $5 AND status = $6
		 RETURNING id`,
		a.Severity, nullString(a.Technique), a.LastSeen, a.TripCount, a.Source, correlate.StatusOpen,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: alarm guncellenemedi: %w", err)
	}

	err = s.db.QueryRowContext(ctx,
		`INSERT INTO alerts (severity, technique, source, status, first_seen, last_seen, trip_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		a.Severity, nullString(a.Technique), a.Source, correlate.StatusOpen, a.FirstSeen, a.LastSeen, a.TripCount,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: alarm yazilamadi: %w", err)
	}
	return id, nil
}

// Alerts, panele beslenmek üzere alarmları en yeni önce döner.
func (s *Store) Alerts(ctx context.Context, limit int) ([]correlate.Alert, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id::text, severity, COALESCE(technique, ''), source, status, first_seen, last_seen, trip_count
		   FROM alerts ORDER BY last_seen DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: alarmlar okunamadi: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]correlate.Alert, 0)
	for rows.Next() {
		var a correlate.Alert
		if err := rows.Scan(&a.ID, &a.Severity, &a.Technique, &a.Source, &a.Status,
			&a.FirstSeen, &a.LastSeen, &a.TripCount); err != nil {
			return nil, fmt.Errorf("store: alarm satiri okunamadi: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: alarm iterasyonu: %w", err)
	}
	return out, nil
}

// nullString, boş string'i SQL NULL'a çevirir.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullTimePtr, nil zaman işaretçisini SQL NULL'a çevirir.
func nullTimePtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return *t
}
