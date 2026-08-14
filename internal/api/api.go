// Package api, GZ0-5: GÖKZİNCİR'in okuma uçlarıdır.
//
// GET /api/v1/alerts, GÖKTÜRK ve GÖKKALKAN ile BİREBİR aynı sözleşmeyi
// döndürür (gokturk-core/correlate.Alert). Panel çoklu kaynağı zaten
// destekliyor (ALERT_SOURCES); sözleşme aynı kaldığı sürece GÖKZİNCİR
// panelde üçüncü kaynak olarak görünür ve panelde kod değişikliği
// gerekmez (issue #8 AC).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/GokturkFK/gokturk-core/correlate"
	"github.com/GokturkFK/gokzincir/internal/blastradius"
	"github.com/GokturkFK/gokzincir/internal/store"
)

// Limit varsayılanları: GÖKKALKAN'daki okuma API'siyle aynı.
const (
	DefaultLimit = 100
	MaxLimit     = 1000
)

// Store, okuma uçlarının ihtiyaç duyduğu sorguları soyutlar.
type Store interface {
	Alerts(ctx context.Context, limit int) ([]correlate.Alert, error)
	Identities(ctx context.Context, limit int) ([]store.IdentityView, error)
	blastradius.Graph
}

// API, okuma uçlarını sunar.
type API struct {
	store  Store
	logger *slog.Logger
}

// New, verilen Store ile bir API kurar.
func New(s Store, logger *slog.Logger) *API {
	return &API{store: s, logger: logger}
}

// Routes, uçları verilen mux'a bağlar.
func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/alerts", a.handleAlerts)
	mux.HandleFunc("GET /api/v1/nhi", a.handleIdentities)
	mux.HandleFunc("GET /api/v1/nhi/{id}/blast-radius", a.handleBlastRadius)
}

func (a *API) handleAlerts(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	alerts, err := a.store.Alerts(r.Context(), limit)
	if err != nil {
		a.logger.Error("alarmlar okunamadi", "err", err)
		a.writeError(w, http.StatusInternalServerError, "alarmlar okunamadi")
		return
	}
	a.writeJSON(w, alerts)
}

func (a *API) handleIdentities(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err := a.store.Identities(r.Context(), limit)
	if err != nil {
		a.logger.Error("envanter okunamadi", "err", err)
		a.writeError(w, http.StatusInternalServerError, "envanter okunamadi")
		return
	}
	a.writeJSON(w, ids)
}

// BlastRadiusResponse, bir NHI'nin hasar yüzeyidir (DoD madde 2).
//
// Bu bir ALARM DEĞİLDİR — salt görünürlük/rapor (docs/DECISIONS.md
// Karar 3). Skor yüksek diye alarm üretilmez; alarm yalnızca bir tuzağa
// fiilen dokunulunca çıkar.
type BlastRadiusResponse struct {
	SourceID  string   `json:"source_id"`
	Reachable []string `json:"reachable"`
	Count     int      `json:"count"`
	Score     float64  `json:"score"`
	MaxDepth  int      `json:"max_depth"`
}

func (a *API) handleBlastRadius(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "id zorunlu")
		return
	}

	res, err := blastradius.Compute(r.Context(), a.store, id)
	if err != nil {
		a.logger.Error("blast-radius hesaplanamadi", "err", err, "id", id)
		a.writeError(w, http.StatusInternalServerError, "blast-radius hesaplanamadi")
		return
	}

	reachable := res.ReachableSorted()
	a.writeJSON(w, BlastRadiusResponse{
		SourceID:  res.SourceID,
		Reachable: reachable,
		Count:     len(reachable),
		Score:     res.Score,
		MaxDepth:  blastradius.MaxDepth,
	})
}

// parseLimit, limit parametresini doğrular.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New("limit pozitif bir tamsayi olmali")
	}
	if n > MaxLimit {
		n = MaxLimit
	}
	return n, nil
}

// writeJSON, boş sonuçlarda da null değil [] döndürür — panel tarafında
// null kontrolü gerekmesin (GÖKKALKAN'daki API ile aynı davranış).
func (a *API) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		a.logger.Error("yanit yazilamadi", "err", err)
	}
}

func (a *API) writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		a.logger.Error("hata yaniti yazilamadi", "err", err)
	}
}
