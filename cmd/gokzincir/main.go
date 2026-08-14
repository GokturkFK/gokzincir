// GÖKZİNCİR binary'si.
//
// Tek HTTP sunucusu:
//   - /healthz
//   - okuma API'si (GET /api/v1/alerts, /api/v1/nhi, /api/v1/nhi/{id}/blast-radius)
//   - envanter yazma ucu (POST /api/v1/inventory)
//   - sahte NHI kullanım bildirimi (POST /api/v1/nhi-usage)
//
// internal/inventory (envanter), internal/store (kalıcılık),
// internal/blastradius (risk), internal/nhitrap (tuzak) ve
// internal/correlator (trip→alarm) burada tek akışta birleşir.
// GÖKKALKAN'da öğrenilen ders: paketler ayrı ayrı test edilip birbirine
// bağlanmazsa DoD kâğıt üstünde kalır.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GokturkFK/gokturk-core/trap"
	"github.com/GokturkFK/gokzincir/internal/api"
	"github.com/GokturkFK/gokzincir/internal/config"
	"github.com/GokturkFK/gokzincir/internal/correlator"
	"github.com/GokturkFK/gokzincir/internal/inventory"
	"github.com/GokturkFK/gokzincir/internal/nhitrap"
	"github.com/GokturkFK/gokzincir/internal/store"
	_ "github.com/lib/pq"
)

// TechniqueFakeNHIUsage, docs/DECISIONS.md Karar 4'te onaylanan ATT&CK
// tekniği: T1078.004 (Valid Accounts: Cloud Accounts).
const TechniqueFakeNHIUsage = "T1078.004"

func main() {
	// Ayni binary "healthcheck" arguman ile calistirilir (distroless imajda
	// wget/curl yok — bkz. gokzincir.Dockerfile). GOKKALKAN ile ayni desen.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(envOrDefault("HTTP_ADDR", ":8100")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config yuklenemedi", "err", err)
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DBDSN)
	if err != nil {
		logger.Error("postgres surucusu acilamadi", "err", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelPing()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Error("postgres'e baglanilamadi", "err", err)
		os.Exit(1)
	}

	st := store.New(db)
	ingester := inventory.NewIngester(st, nil)
	engine := correlator.New(st)
	decoder := nhitrap.NewDecoder(st, newUUIDv4)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	api.New(st, logger).Routes(mux)
	mux.HandleFunc("POST /api/v1/inventory", handleIngest(ingester, logger))
	mux.HandleFunc("POST /api/v1/nhi-usage", handleUsage(decoder, engine, logger))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("gokzincir basladi",
		"http_addr", cfg.HTTPAddr,
		"nats_url", cfg.NATSURL,
		"trip_subject", trap.SubjectTripEvents,
		"technique", TechniqueFakeNHIUsage,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http sunucu hatasi", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("kapatiliyor")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown basarisiz", "err", err)
	}
}

// handleIngest, envanter toplama turunun yazma ucudur (GZ0-2).
func handleIngest(in *inventory.Ingester, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := in.IngestJSON(r.Context(), r.Body)
		if err != nil {
			// Girdi dogrulama hatalari istemcinin duzeltebilecegi seyler;
			// 400 doner ve HICBIR SEY yazilmaz (bkz. inventory.Ingest).
			logger.Warn("envanter alinamadi", "err", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, logger)
			return
		}
		writeJSON(w, http.StatusOK, res, logger)
	}
}

// handleUsage, bir NHI kullanım gözlemini alır ve tuzak tetiklenmişse
// korelasyona besler (GZ0-4).
//
// Meşru kullanım (tuzak olmayan NHI) 200 + triggered=false döner — hata
// değildir. Sıfır-FP'nin uçtaki karşılığı: sensör her kullanımı
// gönderebilir, alarm yalnızca tuzağa dokunulunca çıkar.
func handleUsage(d *nhitrap.Decoder, engine *correlator.Engine, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var usage nhitrap.Usage
		if err := json.NewDecoder(r.Body).Decode(&usage); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gozlem cozumlenemedi"}, logger)
			return
		}
		if usage.ObservedAt.IsZero() {
			usage.ObservedAt = time.Now().UTC()
		}

		line, err := json.Marshal(usage)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gozlem serilestirilemedi"}, logger)
			return
		}

		ev, err := d.Decode(trap.RawObservation{
			Sensor:     "nhi-access-log",
			Line:       string(line),
			ObservedAt: usage.ObservedAt,
		})
		if err != nil {
			if errors.Is(err, trap.ErrNotATrip) {
				writeJSON(w, http.StatusOK, map[string]any{"triggered": false}, logger)
				return
			}
			logger.Warn("gozlem cozumlenemedi", "err", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, logger)
			return
		}

		res, err := engine.Handle(r.Context(), *ev, TechniqueFakeNHIUsage)
		if err != nil {
			logger.Error("korelasyon basarisiz", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "korelasyon basarisiz"}, logger)
			return
		}

		logger.Warn("sahte NHI kullanildi",
			"nhi_id", usage.NHIID, "accessed_by", usage.AccessedBy, "alarm_sayisi", len(res.Alerts))
		writeJSON(w, http.StatusOK, map[string]any{"triggered": true, "alerts": res.Alerts}, logger)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("yanit yazilamadi", "err", err)
	}
}

// newUUIDv4, crypto/rand ile RFC 4122 v4 uuid uretir. Harici bagimlilik
// eklememek icin elle kuruldu (GOKKALKAN'daki ayni desen).
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("main: uuid uretilemedi: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // surum 4
	b[8] = (b[8] & 0x3f) | 0x80 // varyant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func runHealthcheck(addr string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://localhost"+addr+"/healthz", nil)
	if err != nil {
		return 1
	}
	resp, err := client.Do(req)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
