// Package seed, sahte NHI'ların envantere OTOMATİK ekilmesidir —
// GÖKTÜRK'teki OPS-11 (otomatik tuzak dağıtımı) muadili.
//
// Neden var: internal/nhitrap tuzağı üretebiliyordu (Provider.Provision)
// ama üründe onu ÇAĞIRAN kimse yoktu. Yani çalışan sistemde hiçbir zaman
// bir tuzak oluşmuyordu ve DoD madde 3 ("ekilmiş sahte bir NHI kullanılınca
// alarm") ürünün kendi arayüzünden erişilemiyordu — tuzağı ancak elle SQL
// ile eklemek mümkündü. Bu paket o boşluğu kapatır.
//
// Rol sınırı: nhitrap paketi "somut içerik üretimi çağıranın alanı" diyor
// (bkz. nhitrap.ProfileGenerator). Burada yapılan şey ekim MEKANİĞİdir;
// tuzağın tespit-edilemezliğinin ince ayarı (GZ-B1) Cyber'ın alanıdır ve
// bu paket ona karışmaz: profil UYDURULMAZ, gerçek envanterin kendi
// dağılımından örneklenir.
package seed

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/GokturkFK/gokturk-core/trap"
)

// DefaultCount, hedeflenen sahte NHI sayısıdır.
const DefaultCount = 1

// Sample, envanterde HÂLİHAZIRDA bulunan bir profildir.
type Sample struct {
	NHIType string
	Owner   string
	Scope   string
}

// Store, ekim kararı için gereken sorguları soyutlar.
type Store interface {
	// CountDecoys, ekili tuzak sayısıdır (ekimin idempotent olması için).
	CountDecoys(ctx context.Context) (int, error)
	// ProfileSamples, GERÇEK envanterden profil örnekleri döndürür.
	ProfileSamples(ctx context.Context, limit int) ([]Sample, error)
}

// Provisioner, nhitrap.Provider'ın karşıladığı sözleşmedir.
type Provisioner interface {
	Provision(ctx context.Context, createdBy string) (*trap.Trap, *trap.Artifacts, error)
}

// Profiles, nhitrap.ProfileGenerator'ın uygulamasıdır.
//
// Örnek havuzu ekim ANINDA doldurulur (Load) — provider kurulurken değil.
// Sebep: envanter zamanla değişir, tuzak ekildiği andaki envantere
// benzemelidir; boot'ta bir kez okunan sabit bir havuz, aylar sonra
// ekilen tuzağı eski dağılıma göre üretirdi.
type Profiles struct {
	mu      sync.RWMutex
	samples []Sample
	pick    func(n int) (int, error)
}

// NewProfiles, boş bir havuzla başlar. pick nil ise crypto/rand kullanılır.
func NewProfiles(pick func(n int) (int, error)) *Profiles {
	if pick == nil {
		pick = cryptoPick
	}
	return &Profiles{pick: pick}
}

// Load, örnek havuzunu değiştirir.
func (p *Profiles) Load(samples []Sample) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = samples
}

// Generate, nhitrap.ProfileGenerator sözleşmesini karşılar: havuzdan
// rastgele bir profil seçer.
//
// Örnek AYNEN kopyalanır, alanlar farklı kayıtlardan harmanlanmaz: tip,
// sahip ve kapsam gerçek envanterde birlikte görülen bir üçlüdür;
// karıştırmak "machine_identity + owner=billing + scope=admin" gibi
// envanterde hiç görülmeyen ve bu yüzden GÖZE BATAN kombinasyonlar
// üretirdi. Kimlik zaten uuid olduğu için satır tekil kalır.
//
// Havuz boşsa boş üçlü döner; Seeder bu durumda zaten Provision çağırmaz.
func (p *Profiles) Generate() (nhiType, owner, scope string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.samples) == 0 {
		return "", "", ""
	}
	i, err := p.pick(len(p.samples))
	if err != nil || i < 0 || i >= len(p.samples) {
		i = 0
	}
	s := p.samples[i]
	return s.NHIType, s.Owner, s.Scope
}

func cryptoPick(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

// Seeder, hedeflenen tuzak sayısını sağlar.
type Seeder struct {
	store    Store
	profiles *Profiles
	prov     Provisioner
	target   int
}

// New, bir Seeder kurar. target <= 0 ise ekim kapalıdır.
func New(store Store, profiles *Profiles, prov Provisioner, target int) *Seeder {
	return &Seeder{store: store, profiles: profiles, prov: prov, target: target}
}

// Ensure, eksik tuzakları eker ve ekilen tuzakların id'lerini döner.
// İdempotenttir: hedefe ulaşılmışsa hiçbir şey yapmaz, bu yüzden her
// envanter turundan sonra güvenle çağrılabilir.
//
// ENVANTER BOŞSA EKİM ERTELENİR (nil, nil). Boş bir envantere ekilen tuzak,
// benzeyeceği hiçbir gerçek kayıt olmadığı için tek başına durur ve
// saldırgan için ilk bakışta ayırt edilebilir olurdu; ürünün tezi tuzağın
// envanterin İÇİNDE kaybolmasına dayanıyor (docs/DECISIONS.md Karar 5).
// Bu yüzden ekim, ilk envanter turu geldikten sonra kendiliğinden olur.
func (s *Seeder) Ensure(ctx context.Context) ([]string, error) {
	if s.target <= 0 {
		return nil, nil
	}

	have, err := s.store.CountDecoys(ctx)
	if err != nil {
		return nil, fmt.Errorf("seed: tuzak sayisi okunamadi: %w", err)
	}
	if have >= s.target {
		return nil, nil
	}

	samples, err := s.store.ProfileSamples(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("seed: envanter profilleri okunamadi: %w", err)
	}
	if len(samples) == 0 {
		return nil, nil
	}
	s.profiles.Load(samples)

	var planted []string
	for i := have; i < s.target; i++ {
		t, _, err := s.prov.Provision(ctx, "gokzincir-seeder")
		if err != nil {
			return planted, fmt.Errorf("seed: tuzak ekilemedi: %w", err)
		}
		planted = append(planted, t.ID)
	}
	return planted, nil
}
