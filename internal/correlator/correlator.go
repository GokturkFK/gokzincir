// Package correlator, GZ0-4: sahte NHI tetiklenince üretilen TripEvent'i
// gokturk-core/correlate'e besleyip alarmı kalıcı yazan wiring katmanıdır.
//
// GÖKKALKAN'daki internal/enforce'un muadili — ama v0.1 kapsamı
// GÖRÜNÜRLÜK olduğu için burada otomatik kesme (remediation) YOKTUR
// (PROJECT_PLAN.md böl. 2: "otomatik remediation → GÖKKALKAN'ın alanı,
// v0.1 kapsamı görünürlük"). Kimliği kesmek/rotate etmek v0.2'ye ait.
package correlator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokturk-core/trap"
)

// DefaultWindow, korelasyon penceresidir. gokturk-core/correlate pencereyi
// seçmeyi çağırana bırakır; GÖKKALKAN ile aynı değer kullanılıyor ki
// aynı panelde birleşen alarmlar tutarlı davransın.
const DefaultWindow = 30 * time.Minute

// Store, korelasyonun ihtiyaç duyduğu kalıcılık işlemlerini soyutlar.
type Store interface {
	InsertTripEvent(ctx context.Context, ev trap.TripEvent) error
	TripsBySource(ctx context.Context, source string, window time.Duration, now time.Time) ([]trap.TripEvent, error)
	UpsertAlert(ctx context.Context, a correlate.Alert) (string, error)
}

// ErrNoSource, TripEvent.Source boşken döner: korelasyon kaynağa göre
// gruplandığı için kaynaksız bir trip değerlendirilemez.
var ErrNoSource = errors.New("correlator: TripEvent.Source bos, korelasyon yapilamaz")

// Engine, trip → alarm akışını yürütür.
type Engine struct {
	store  Store
	window time.Duration
	now    func() time.Time
}

// Option, Engine kurulumunu özelleştirir.
type Option func(*Engine)

// WithWindow, korelasyon penceresini değiştirir.
func WithWindow(d time.Duration) Option { return func(e *Engine) { e.window = d } }

// WithClock, saat kaynağını değiştirir (testler için).
func WithClock(now func() time.Time) Option { return func(e *Engine) { e.now = now } }

// New, verilen Store ile bir Engine kurar.
func New(store Store, opts ...Option) *Engine {
	e := &Engine{store: store, window: DefaultWindow, now: time.Now}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Result, bir trip'in işlenmesinin sonucudur.
type Result struct {
	// Alerts, bu trip'ten sonra kaynağın güncel alarm durumudur
	// (kaynak başına tek alarm — kampanya birleşmesi).
	Alerts []correlate.Alert
}

// Handle, bir TripEvent'i kalıcı yazar, korelasyon penceresindeki
// trip'leri değerlendirir ve alarmı upsert eder.
//
// technique çağıran tarafından verilir (docs/DECISIONS.md Karar 4:
// T1078.004). Sabit kodlanmaz — gokturk-core/correlate'in sözleşmesi
// tekniği dışarıdan almak üzerine kurulu ve ürünler farklı teknikler
// kullanıyor.
func (e *Engine) Handle(ctx context.Context, ev trap.TripEvent, technique string) (Result, error) {
	if ev.Source == "" {
		return Result{}, ErrNoSource
	}

	if err := e.store.InsertTripEvent(ctx, ev); err != nil {
		return Result{}, fmt.Errorf("correlator: trip kaydedilemedi: %w", err)
	}

	now := e.now()
	trips, err := e.store.TripsBySource(ctx, ev.Source, e.window, now)
	if err != nil {
		return Result{}, fmt.Errorf("correlator: trip'ler alinamadi: %w", err)
	}

	alerts := correlate.Evaluate(trips, technique)
	res := Result{Alerts: alerts}
	for i := range alerts {
		id, err := e.store.UpsertAlert(ctx, alerts[i])
		if err != nil {
			return res, fmt.Errorf("correlator: alarm yazilamadi: %w", err)
		}
		alerts[i].ID = id
	}
	return res, nil
}
