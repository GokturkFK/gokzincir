# GÖKZİNCİR

NHI Governance & Blast-Radius Graph — insan-olmayan kimliklerin (servis
hesapları, token'lar, makine kimlikleri) envanteri, ilişki grafiği ve bir
kimlik ele geçtiğinde oluşacak hasar yüzeyi.
Göktürk platformunun P3'ü (bkz. [gokturk-core](https://github.com/GokturkFK/gokturk-core),
[gokturk-deception-mesh](https://github.com/GokturkFK/gokturk-deception-mesh),
[gokkalkan](https://github.com/GokturkFK/gokkalkan)).
Görev dökümü: [PROJECT_PLAN.md](PROJECT_PLAN.md).

**Tek cümlelik hedef:** Ele geçirilmiş bir servis hesabıyla yanal hareket
etmeye çalışan bir saldırgan, **blast-radius grafiğinde** gerçekte nereye
erişebildiğiyle görünür olur; ekilmiş **sahte bir NHI**'ya dokunduğu anda
panelde **Critical** alarm belirir — meşru servis-hesabı kullanımı ise
hiçbir alarm üretmez.

> Durum: **Cyber ve DevOps tarafı tamamlandı.** Zincir uçtan uca çalışıyor:
> envanter → graph/blast-radius → tuzak ekimi → tuzağa dokunma → korelasyon
> → alarm → panel. Tek eksik DoD maddesi demo GIF ([#4](../../issues/4),
> Cyber) — `scripts/demo.sh` senaryoyu tek komutta koşturuyor.

## Nereden başlanır

**Tüm işler [issue](../../issues) olarak açık ve atanmış.** Etiketler:
`cyber` = @fetihcakmak (güvenlik/risk çekirdeği), `devops` = @uzunkubra50
(platform/teslimat/hat). Hangi dosya kimin: [CODEOWNERS](CODEOWNERS).

| Sıra | Issue | Kim | Durum |
|---|---|---|---|
| 1 | [#1 GZ-S0](../../issues/1) — Sprint 0 tasarım kararı | @fetihcakmak | ✅ tamamlandı |
| 2 | [#2 GZ-A2](../../issues/2) blast-radius algoritması | @fetihcakmak | ✅ tamamlandı |
| 3 | [#3 GZ-B1](../../issues/3) sahte NHI provider'ı | @fetihcakmak | ✅ tamamlandı |
| 4 | [#4 GZ-F1](../../issues/4) tehdit modeli + demo | @fetihcakmak | tehdit modeli ✅, **demo GIF açık** (zincir hazır) |
| 5 | [#5 GZ0-2](../../issues/5) envanter toplama hattı | @uzunkubra50 | ✅ tamamlandı |
| 6 | [#6 GZ0-3](../../issues/6) graph deposu + sorgu | @uzunkubra50 | ✅ tamamlandı |
| 7 | [#7 GZ0-4](../../issues/7) korelasyon wiring | @uzunkubra50 | ✅ tamamlandı |
| 8 | [#8 GZ0-5](../../issues/8) okuma API'si | @uzunkubra50 | ✅ tamamlandı |

## Uçtan uca deneme

```sh
cp deployments/docker/.env.example deployments/docker/.env
make docker-up          # ayri bir terminalde
./scripts/demo.sh
```

`demo.sh` DoD'nin 1–4. maddelerini **çalışan sistem üzerinde** sırayla
gösterir: envanteri doldurur, blast-radius'u okur, meşru bir NHI kullanımının
**hiçbir alarm üretmediğini** (sıfır-FP) ve ekilen sahte NHI'ya dokununca
`T1078.004` / **Critical** alarmın düştüğünü kanıtlar. İkinci dokunuş ayrı
bir alarm açmaz, aynı kampanyaya birleşir.

Migration'lar compose ayağa kalkarken tek seferlik `migrate` servisi
tarafından uygulanır — host'ta `goose` kurulu olması gerekmez (bkz.
[deployments/docker/migrate.sh](deployments/docker/migrate.sh)).

## API

| Uç | Ne yapar |
|---|---|
| `GET /healthz` | sağlık |
| `GET /api/v1/alerts` | alarmlar — GÖKTÜRK/GÖKKALKAN ile **birebir aynı** sözleşme |
| `GET /api/v1/nhi` | NHI envanteri (tuzaklar ve sır özetleri **dönmez**) |
| `GET /api/v1/nhi/{id}/blast-radius` | hasar yüzeyi + skor (alarm değil, salt görünürlük) |
| `POST /api/v1/inventory` | envanter toplama turu (idempotent) |
| `POST /api/v1/nhi-usage` | NHI kullanım gözlemi; tuzaksa korelasyona düşer |

## Sahte NHI ekimi

Tuzak envantere **kendiliğinden** ekilir (`DECOY_COUNT`, varsayılan 1) —
GÖKTÜRK'teki otomatik tuzak dağıtımının (OPS-11) muadili. İki bilinçli
davranış:

- **Ekim ilk envanter turundan SONRA olur.** Tuzağın profili uydurulmaz,
  gerçek envanterin kendi dağılımından örneklenir; boş bir envantere ekilen
  tuzak benzeyeceği hiçbir kayıt olmadığı için tek başına dururdu.
- **Tuzağın id'si hiçbir API yanıtında dönmez**, yalnızca operatör log'una
  yazılır (`"msg":"sahte NHI ekildi"`). Okuma uçları tuzakları listelemez;
  aksi hâlde "envanterde ayırt edilemez durma" tezini panelin kendisi bozardı.

## Panele bağlama (üçüncü kaynak)

GÖKTÜRK panelinde `ALERT_SOURCES` ayarlanır; panelde **kod değişikliği
gerekmez** (sözleşme aynı):

```sh
ALERT_SOURCES="GÖKTÜRK=http://control-api:8080,GÖKZİNCİR=http://host.docker.internal:8100"
```

## Mimari

```
   NHI kaynaklari ──▶ envanter hatti (GZ0-2) ──▶ Postgres
                                                    │
                                    ┌───────────────┴───────────────┐
                                    ▼                               ▼
                          graph + blast-radius              sahte NHI (GZ-B1)
                             (GZ0-3 / GZ-A2)                        │
                                    │                     dokunulunca TripEvent
                                    │                               │
                                    ▼                               ▼
                            okuma API'si (GZ0-5) ◀──── correlate.Evaluate (GZ0-4)
                                    │                     [gokturk-core]
                                    ▼
                     GOKTURK SOC paneli (ayni feed, ucuncu kaynak)
```

`gokturk-core` **import edilir, kopyalanmaz** — `trap.Provider`,
`trap.TripEvent`, `correlate.Alert`, `correlate.Evaluate` tek doğruluk
kaynağıdır.

## Geliştirme

```sh
make build     # go build ./...
make test      # go test -race
make lint      # golangci-lint
make vet
```

## Stack'i ayağa kaldırma

```sh
cp deployments/docker/.env.example deployments/docker/.env
make docker-up
```

Postgres **5434**, NATS **4224/8224** portunda açılır (GÖKTÜRK 5432,
GÖKKALKAN 5433 kullanıyor — üçü aynı makinede aynı anda ayağa kalkabilsin
diye).

## Migration'lar

Compose kullanıyorsan elle bir şey yapman gerekmez (yukarıdaki `migrate`
servisi). Host'tan çalıştırmak için `goose` gerekir:

```sh
make migrate-up
make migrate-down
```

İki yol aynı sürüm tablosunu (`goose_db_version`) kullanır, birbirinin
üstüne uygulamaz.

`00001_init.sql` bilinçli olarak yalnızca `trip_events`/`alerts` içerir
(gokturk-core şemasıyla birebir). `00002_nhi.sql`, Sprint 0 kararı
onaylandıktan sonra NHI/graph tablolarını (`nhi_identities`, `nhi_edges`)
ekler.

## v0.1 Definition of Done

| # | Madde | Durum |
|---|---|---|
| 1 | Envanter + ilişki grafiği kurulabiliyor | ✅ `scripts/demo.sh` adım 1 |
| 2 | Blast-radius API'den okunabiliyor | ✅ adım 2 |
| 3 | Sahte NHI kullanılınca ≤5 sn'de alarm | ✅ adım 4 (anlık) |
| 4 | Meşru NHI kullanımı → alarm yok | ✅ adım 3 |
| 5 | `go test -race` yeşil, kapsam ≥ %70 | ✅ CI'da %83.5 |
| 6 | README + tehdit modeli + demo GIF | tehdit modeli ✅, GIF [#4](../../issues/4) |
| 7 | CI PR'ı bloklayabiliyor | ✅ branch protection açık |

## Branch & PR kuralları

`main` korumalı: doğrudan push kapalı, PR + yeşil CI zorunlu, squash-only.
