// Package nhitrap, GZ-B1: sahte NHI (non-human identity) provider'ını
// uygular.
//
// gokturk-core/trap.Provider'ın GÖKZİNCİR'e özgü uygulaması — GÖKTÜRK'teki
// CredentialCanaryProvider ve GÖKKALKAN'daki agent honeypot ile aynı desen:
// gerçek bir servis hesabı gibi duran ama kullanılınca TripEvent üreten bir
// tuzak; tip sabiti core'a taşınmadı (ürüne özgü).
//
// docs/DECISIONS.md Karar 5: sahte NHI, ayrı bir tuzak tablosunda değil,
// nhi_identities'in içinde is_decoy=true bir satır olarak tutulur. DB
// erişimi bilerek Store arayüzü arkasına soyutlandı — gerçek Postgres
// implementasyonu wiring aşamasının (GZ0-4) işi.
package nhitrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GokturkFK/gokturk-core/trap"
)

// TypeDecoyNHI, bu ürüne özgü tuzak tipi (GÖKKALKAN'daki TypeAgentHoneypot,
// GÖKTÜRK'teki TypeCredentialCanary'nin muadili).
const TypeDecoyNHI = "decoy_nhi"

// Identity, bir NHI kaydıdır — nhi_identities şemasına (migrations/00002)
// uygun. Tablo hem GERÇEK envanteri hem tuzakları tutar (docs/DECISIONS.md
// Karar 5: ayrı bir decoy tablosu yok), bu yüzden ayrımı IsDecoy taşır.
type Identity struct {
	ID            string
	NHIType       string // "service_account" / "token" / "machine_identity"
	Owner         string
	Scope         string
	SecretRefHash string
	CreatedAt     time.Time
	// IsDecoy, bu satırın GZ-B1'in ektiği bir tuzak olup olmadığıdır
	// (nhi_identities.is_decoy). Decoder YALNIZCA bu alanı true olan
	// kayıtlar için TripEvent üretir — gerçek envanter kayıtlarının meşru
	// kullanımı hiçbir şey tetiklemez (sıfır-FP).
	//
	// Alan hiçbir API/panel çıktısına sızmaz (docs/DECISIONS.md Karar 1);
	// okuma uçları bunu döndürmez, yalnızca bu paket ve iç sorgular bakar.
	IsDecoy bool
}

// Store, nhi_identities tablosuna erişimi soyutlar. Gerçek Postgres
// implementasyonu bu paketin dışında (wiring), testler için sahte bir
// Store yeterlidir.
type Store interface {
	Create(ctx context.Context, id Identity) error
	FindByID(ctx context.Context, nhiID string) (*Identity, error)
}

// ErrIdentityNotFound, Store.FindByID bir eşleşme bulamadığında döner.
var ErrIdentityNotFound = errors.New("nhitrap: nhi bulunamadi")

// ProfileGenerator, provision anında "inandırıcı" owner/scope üretir.
// GZ-B1'in tespit-edilemezlik işi burada değil — somut içerik (gerçek
// envanterin dağılımına ne kadar benzeyeceği) çağıranın alanı; bu paket
// sadece üretileni nhi_identities'e yazıp TripEvent üretir.
type ProfileGenerator interface {
	Generate() (nhiType, owner, scope string)
}

// Provider, gokturk-core/trap.Provider'ı uygular: her çağrıda yeni bir
// sahte NHI provision eder.
type Provider struct {
	store    Store
	profiles ProfileGenerator
	idFn     func() string
	now      func() time.Time
}

// NewProvider, verilen Store ve ProfileGenerator ile bir Provider kurar.
// idFn zorunludur (örn. production'da google/uuid.NewString) — nil
// bırakılırsa Provision hata döner. now nil bırakılırsa time.Now kullanılır.
func NewProvider(store Store, profiles ProfileGenerator, idFn func() string, now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{store: store, profiles: profiles, idFn: idFn, now: now}
}

// Provision, gokturk-core/trap.Provider sözleşmesini karşılar: yeni bir
// sahte NHI üretir, Store'a yazar, Trap kaydını döner. Artifacts burada
// anlamsız (credential canary'nin aksine agent'a gizlice verilecek bir
// secret yok — sahte NHI zaten envanterin kendisinde görünür), bu yüzden
// boş döner; sözleşme bunu zorunlu kılmıyor.
func (p *Provider) Provision(ctx context.Context, createdBy string) (*trap.Trap, *trap.Artifacts, error) {
	if p.idFn == nil {
		return nil, nil, errors.New("nhitrap: idFn tanimli degil")
	}

	nhiType, owner, scope := p.profiles.Generate()
	id := p.idFn()
	now := p.now()

	identity := Identity{
		ID:        id,
		NHIType:   nhiType,
		Owner:     owner,
		Scope:     scope,
		CreatedAt: now,
		// Tuzak olarak isaretlenmesi ZORUNLU: nhi_identities.is_decoy
		// varsayilani false, isaretlenmezse ekilen tuzak gercek envanter
		// kaydindan ayirt edilemez ve Decoder onu hic tetikleyemezdi.
		IsDecoy: true,
	}

	if err := p.store.Create(ctx, identity); err != nil {
		return nil, nil, fmt.Errorf("nhitrap: nhi kaydedilemedi: %w", err)
	}

	metadata, err := json.Marshal(struct {
		NHIType string `json:"nhi_type"`
		Owner   string `json:"owner"`
		Scope   string `json:"scope"`
	}{nhiType, owner, scope})
	if err != nil {
		return nil, nil, fmt.Errorf("nhitrap: metadata marshal hatasi: %w", err)
	}

	t := &trap.Trap{
		ID:        id,
		Type:      TypeDecoyNHI,
		Username:  owner,
		Metadata:  metadata,
		CreatedBy: createdBy,
		CreatedAt: now,
	}

	return t, &trap.Artifacts{}, nil
}

// Usage, sahte bir NHI'nin kullanıldığı ham gözlemdir. Sensör (envanter
// hattı/erişim log'u) bunu Decode'a geçirir.
type Usage struct {
	NHIID        string
	AccessedBy   string
	TargetNodeID string
	ObservedAt   time.Time
}

// Decoder, gokturk-core/trap.Decoder'ı uygular: bir NHI kullanımının
// nhi_identities'teki bir tuzağı tetikleyip tetiklemediğine bakar.
// Eşleşme yoksa ErrNotATrip döner — meşru NHI kullanımı hiçbir event
// üretmez (sıfır-FP, issue #3 AC'sinin karşılığı).
type Decoder struct {
	store Store
	idFn  func() string
}

// NewDecoder, verilen Store ile bir Decoder kurar.
func NewDecoder(store Store, idFn func() string) *Decoder {
	return &Decoder{store: store, idFn: idFn}
}

// Decode, gokturk-core/trap.Decoder sözleşmesini karşılar.
func (d *Decoder) Decode(obs trap.RawObservation) (*trap.TripEvent, error) {
	if d.idFn == nil {
		return nil, errors.New("nhitrap: idFn tanimli degil")
	}

	usage, err := parseUsage(obs)
	if err != nil {
		return nil, fmt.Errorf("nhitrap: gozlem cozumlenemedi: %w", err)
	}

	identity, err := d.store.FindByID(context.Background(), usage.NHIID)
	if err != nil {
		if errors.Is(err, ErrIdentityNotFound) {
			return nil, trap.ErrNotATrip
		}
		return nil, fmt.Errorf("nhitrap: store sorgusu basarisiz: %w", err)
	}

	// nhi_identities hem GERCEK envanteri hem tuzaklari tutar
	// (docs/DECISIONS.md Karar 5). Kimligin sadece BULUNMUS olmasi bir
	// tetiklenme degildir — yalnizca tuzaga dokunulmasi TripEvent uretir.
	// Bu kontrol olmadan envanterdeki her gercek NHI'nin mesru kullanimi
	// alarm uretirdi (bkz. TestDecoder_Decode_RealNHIInInventoryIsNotATrip).
	if !identity.IsDecoy {
		return nil, trap.ErrNotATrip
	}

	raw, err := json.Marshal(usage)
	if err != nil {
		return nil, fmt.Errorf("nhitrap: raw marshal hatasi: %w", err)
	}

	return &trap.TripEvent{
		EventID:    d.idFn(),
		TrapID:     identity.ID,
		Sensor:     obs.Sensor,
		Source:     usage.AccessedBy,
		ObservedAt: obs.ObservedAt,
		Raw:        raw,
	}, nil
}

// parseUsage, RawObservation.Line'daki JSON'u Usage'a çevirir. Sensörün
// (envanter/erişim hattı) kullanımı bu şekilde serialize ettiği varsayılır.
func parseUsage(obs trap.RawObservation) (*Usage, error) {
	var u Usage
	if err := json.Unmarshal([]byte(obs.Line), &u); err != nil {
		return nil, err
	}
	if u.NHIID == "" {
		return nil, errors.New("nhi_id bos olamaz")
	}
	return &u, nil
}
