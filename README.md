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

> Durum: **Cyber tarafı tamamlandı** (Sprint 0 kararları onaylı, NHI/edge
> şeması migration'da, `internal/blastradius` ve `internal/nhitrap` yazıldı
> ve test edildi). Sırada DevOps tarafı var: envanter hattı, graph deposu,
> korelasyon wiring, okuma API'si (aşağıdaki tabloya bakınız) — bunlar
> bitmeden paketler tek bir çalışan binary'de birbirine bağlı değil.

## Nereden başlanır

**Tüm işler [issue](../../issues) olarak açık ve atanmış.** Etiketler:
`cyber` = @fetihcakmak (güvenlik/risk çekirdeği), `devops` = @uzunkubra50
(platform/teslimat/hat). Hangi dosya kimin: [CODEOWNERS](CODEOWNERS).

| Sıra | Issue | Kim | Durum |
|---|---|---|---|
| 1 | [#1 GZ-S0](../../issues/1) — Sprint 0 tasarım kararı | @fetihcakmak | ✅ tamamlandı |
| 2 | [#2 GZ-A2](../../issues/2) blast-radius algoritması | @fetihcakmak | ✅ tamamlandı |
| 3 | [#3 GZ-B1](../../issues/3) sahte NHI provider'ı | @fetihcakmak | ✅ tamamlandı |
| 4 | [#4 GZ-F1](../../issues/4) tehdit modeli + demo | @fetihcakmak | tehdit modeli ✅, demo GIF #7'ye bağımlı |
| 5 | [#5 GZ0-2](../../issues/5) envanter toplama hattı | @uzunkubra50 | sırada |
| 6 | [#6 GZ0-3](../../issues/6) graph deposu + sorgu | @uzunkubra50 | #2 hazır, sırada |
| 7 | [#7 GZ0-4](../../issues/7) korelasyon wiring | @uzunkubra50 | #3 hazır, sırada |
| 8 | [#8 GZ0-5](../../issues/8) okuma API'si | @uzunkubra50 | #7'ye bağımlı |

Cyber tarafının (Sprint 0 + GZ-A2 + GZ-B1) tamamlanmasıyla DevOps tarafındaki
blokaj kalktı — GZ0-2/3/4/5 artık paralel ilerleyebilir.

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
(gokturk-core şemasıyla birebir). `00002_nhi.sql`, Sprint 0 kararı
onaylandıktan sonra NHI/graph tablolarını (`nhi_identities`, `nhi_edges`)
ekler.

## Branch & PR kuralları

`main` korumalı: doğrudan push kapalı, PR + yeşil CI zorunlu, squash-only.
