package config

import "testing"

func TestLoad_MissingDBDSN(t *testing.T) {
	t.Setenv("DB_DSN", "")
	if _, err := Load(); err == nil {
		t.Fatal("DB_DSN bos iken hata bekleniyordu")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x/y")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("NATS_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if cfg.HTTPAddr != ":8100" {
		t.Errorf("HTTPAddr = %q, istenen :8100", cfg.HTTPAddr)
	}
	if cfg.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL = %q, istenen varsayilan", cfg.NATSURL)
	}
}

func TestLoad_AllPresent(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x/y")
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("NATS_URL", "nats://custom:4222")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.NATSURL != "nats://custom:4222" || cfg.DBDSN != "postgres://x/y" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoad_DecoyCountDefault(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x/y")
	t.Setenv("DECOY_COUNT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if cfg.DecoyCount != 1 {
		t.Errorf("DecoyCount = %d, istenen 1 (varsayilan ekim acik)", cfg.DecoyCount)
	}
}

func TestLoad_DecoyCountExplicit(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x/y")
	for _, tc := range []struct {
		raw  string
		want int
	}{{"0", 0}, {"3", 3}} {
		t.Setenv("DECOY_COUNT", tc.raw)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("DECOY_COUNT=%q: %v", tc.raw, err)
		}
		if cfg.DecoyCount != tc.want {
			t.Errorf("DECOY_COUNT=%q -> %d, istenen %d", tc.raw, cfg.DecoyCount, tc.want)
		}
	}
}

// Bozuk deger SESSIZCE varsayilana dusmemeli: "ekim var" sanilirken
// olmamasi (ya da tersi) operatoru yanilticidir.
func TestLoad_DecoyCountInvalidFails(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x/y")
	for _, bad := range []string{"bir", "-1", "1.5"} {
		t.Setenv("DECOY_COUNT", bad)
		if _, err := Load(); err == nil {
			t.Errorf("DECOY_COUNT=%q icin hata bekleniyordu", bad)
		}
	}
}
