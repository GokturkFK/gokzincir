// Package config, GÖKZİNCİR'in ortam değişkeni tabanlı ayarlarını yükler.
//
// Bilinçli olarak dar tutuldu: bu, GZ0-1 (proje iskeleti) kapsamı — NHI
// envanterinin hangi kaynaklardan toplanacağı ve graph deposunun ayarları
// GZ-A/GZ-B'nin güvenlik/mimari tasarımının parçası, burada tanımlanmaz.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/GokturkFK/gokzincir/internal/seed"
)

// Config, çekirdek boot için gereken minimum ayardır.
type Config struct {
	HTTPAddr string
	DBDSN    string
	NATSURL  string
	// DecoyCount, envantere ekilecek sahte NHI sayısıdır (bkz.
	// internal/seed). 0 = ekim kapalı.
	DecoyCount int
}

// Load, ortam değişkenlerinden Config üretir. DB_DSN zorunludur.
func Load() (Config, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return Config{}, fmt.Errorf("config: DB_DSN zorunlu")
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8100"
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	decoys, err := decoyCount()
	if err != nil {
		return Config{}, err
	}

	return Config{HTTPAddr: addr, DBDSN: dsn, NATSURL: natsURL, DecoyCount: decoys}, nil
}

// decoyCount, DECOY_COUNT'u çözer.
//
// Geçersiz bir değer SESSİZCE varsayılana düşmez, hata döner: "DECOY_COUNT=bir"
// yazan bir operatör tuzağın ekildiğini sanırdı; sessiz düşüş burada
// "koruma var sanılırken yok" demek olurdu.
func decoyCount() (int, error) {
	raw := os.Getenv("DECOY_COUNT")
	if raw == "" {
		return seed.DefaultCount, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("config: DECOY_COUNT negatif olmayan bir tamsayi olmali, gelen: %q", raw)
	}
	return n, nil
}
