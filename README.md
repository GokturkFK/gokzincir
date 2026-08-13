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

> Durum: **iskelet**. `cmd/gokzincir` boot ediyor (config + `/healthz` +
> graceful shutdown), CI/branch protection/migration taslağı hazır.
> NHI envanteri, graph ve blast-radius mantığı (GZ-A, GZ-B) **henüz
> yazılmadı** — önce Sprint 0 kararları donmalı (aşağı bakınız).

## Nereden başlanır

**Tüm işler [issue](../../issues) olarak açık ve atanmış.** Etiketler:
`cyber` = @fetihcakmak (güvenlik/risk çekirdeği), `devops` = @uzunkubra50
(platform/teslimat/hat). Hangi dosya kimin: [CODEOWNERS](CODEOWNERS).

| Sıra | Issue | Kim | Durum |
|---|---|---|---|
| **1 (BLOCKER)** | [#1 GZ-S0](../../issues/1) — Sprint 0 tasarım kararı | @fetihcakmak | NHI modeli + kenar semantiği + blast-radius tanımı + ATT&CK eşlemesi |
| 2 | [#2 GZ-A2](../../issues/2) blast-radius algoritması | @fetihcakmak | #1'e bağımlı |
| 3 | [#3 GZ-B1](../../issues/3) sahte NHI provider'ı | @fetihcakmak | #1'e bağımlı |
| 4 | [#4 GZ-F1](../../issues/4) tehdit modeli + demo | @fetihcakmak | sona doğru |
| 5 | [#5 GZ0-2](../../issues/5) envanter toplama hattı | @uzunkubra50 | #1'e bağımlı |
| 6 | [#6 GZ0-3](../../issues/6) graph deposu + sorgu | @uzunkubra50 | #1, #2'ye bağımlı |
| 7 | [#7 GZ0-4](../../issues/7) korelasyon wiring | @uzunkubra50 | #3'e bağımlı |
| 8 | [#8 GZ0-5](../../issues/8) okuma API'si | @uzunkubra50 | #7'ye bağımlı |

**Sprint 0 (#1) kapanmadan GZ-A/GZ-B kod yazımı başlamaz** — GÖKTÜRK ve
GÖKKALKAN'da da aynı kural uygulandı: şema/sözleşme donmadan üstüne kod
yazılmaz.

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

Postgres **5434** portunda açılır (GÖKTÜRK 5432, GÖKKALKAN 5433 kullanıyor —
üçü aynı makinede aynı anda ayağa kalkabilsin diye).

## Migration'lar

```sh
make migrate-up
make migrate-down
```

`00001_init.sql` bilinçli olarak yalnızca `trip_events`/`alerts` içerir
(gokturk-core şemasıyla birebir). NHI/graph tabloları Sprint 0 kararı
onaylanınca eklenir.

## Branch & PR kuralları

`main` korumalı: doğrudan push kapalı, PR + yeşil CI zorunlu, squash-only.
