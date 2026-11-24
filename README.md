<div align="center">

# GÖKZİNCİR

**İnsan-Olmayan Kimlik (NHI) Yönetişimi ve Blast-Radius Grafiği (Göktürk Platform P3)**

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Graf%20%26%20Olaylar-4169E1?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Coverage](https://img.shields.io/badge/Test%20Kapsam%C4%B1-%2583.5-22c55e?style=flat-square)](https://github.com/GokturkFK/gokzincir)
[![MITRE ATT&CK](https://img.shields.io/badge/MITRE%20ATT%26CK-T1078.004-C7252B?style=flat-square)](docs/THREAT_MODEL.md)
[![Org](https://img.shields.io/badge/Org-GokturkFK-0d1117?style=flat-square)](https://github.com/GokturkFK)

*Servis hesapları, token'lar ve makine kimliklerinin envanterini çıkarın; ele geçirilme hâlindeki gerçek hasar yüzeyini (blast radius) haritalayın ve ayırt edilemeyen sahte NHI tuzaklarıyla yanal hareketi anında sıfır yanlış pozitif ile yakalayın.*

**🇹🇷 Türkçe** · [🇬🇧 English](#-english)

</div>

---

## Genel Bakış

**GÖKZİNCİR**, Göktürk Güvenlik Platformu'nun 3. sütunudur (P3). **İnsan-olmayan kimlik (NHI) görünürlüğünü** **aktif aldatma (deception)** ile birleştirir:

- **Blast-Radius Grafiği:** Makine kimliklerini (`nhi_identities`) ve erişim/yetki kenarlarını (`nhi_edges`) haritalayarak ele geçirilen bir servis hesabının gerçekte nereye ulaşabileceğini görünür kılar.
- **Gizli Sahte NHI Ekimi:** İlk envanter turundan **sonra** gerçek dağılımdan örnekleyerek otomatik sahte kimlikler (`DECOY_COUNT`, varsayılan `1`) eker.
- **Sıfır-FP Tespit:** Meşru servis hesabı kullanımı hiç alarm üretmez; sahte bir NHI'ya dokunulduğu anda [`gokturk-core`](https://github.com/GokturkFK/gokturk-core) üzerinden `T1078.004` / **Critical** alarm oluşur ve tekrar denemeler tek kampanyada birleşir.

> **Gizlilik Garantisi:** Sahte NHI kimlikleri hiçbir okuma API'sinde (`GET /api/v1/nhi`) dönmez; yalnızca operatör log'una yazılır (`"msg":"sahte NHI ekildi"`). Böylece saldırgan envanter API'sini inceleyerek tuzakları ayırt edemez.

---

## HTTP API

| Uç | İşlev |
| :--- | :--- |
| `GET /healthz` | Sağlık kontrolü |
| `GET /api/v1/alerts` | Alarmlar — GÖKTÜRK (P1) ve GÖKKALKAN (P2) ile **birebir aynı** sözleşme |
| `GET /api/v1/nhi` | NHI envanteri (tuzaklar ve sır özetleri dönmez) |
| `GET /api/v1/nhi/{id}/blast-radius` | Erişim grafiği ve hasar yüzeyi skoru (salt görünürlük, alarm üretmez) |
| `POST /api/v1/inventory` | Idempotent envanter toplama turu |
| `POST /api/v1/nhi-usage` | NHI kullanım gözlemi; tuzaksa korelasyona yönlendirilir |

### Birleşik Göktürk SOC Paneline Bağlama

Panelde kod değişikliği gerekmeden `ALERT_SOURCES` üzerinden üçüncü kaynak olarak eklenir:

```sh
ALERT_SOURCES="GÖKTÜRK=http://control-api:8080,GÖKZİNCİR=http://host.docker.internal:8100"
```

---

## Hızlı Başlangıç ve Uçtan Uca Demo

```sh
cp deployments/docker/.env.example deployments/docker/.env
make docker-up          # ayrı terminalde (Postgres :5434, NATS :4224/8224)
./scripts/demo.sh       # envanter -> blast-radius -> meşru kullanım (0 alarm) -> sahte NHI (Critical)
```

> Portlar P1 (5432) ve P2 (5433) ile çakışmayacak şekilde ayarlanmıştır; üç sütun aynı makinede eşzamanlı çalışabilir. Migration'lar Compose içindeki tek seferlik `migrate` servisiyle otomatik uygulanır (`goose_db_version`).

### Geliştirme, Test ve Migration

```sh
make build     # go build ./...
make test      # go test -race ./... (CI'da %83.5 kapsam)
make lint      # golangci-lint
make vet
make migrate-up
make migrate-down
```

---

## v0.1 Definition of Done

| # | Madde | Durum |
| :---: | :--- | :--- |
| 1 | Envanter + ilişki grafiği kurulabiliyor | ✅ `scripts/demo.sh` adım 1 |
| 2 | Blast-radius API'den okunabiliyor | ✅ adım 2 |
| 3 | Sahte NHI kullanımı → ≤5 sn'de alarm | ✅ adım 4 (anlık) |
| 4 | Meşru NHI kullanımı → alarm yok | ✅ adım 3 |
| 5 | `go test -race` yeşil, kapsam ≥ %70 | ✅ CI'da %83.5 |
| 6 | README + tehdit modeli + demo GIF | tehdit modeli ✅, GIF [#4](../../issues/4) |
| 7 | CI PR'ı bloklayabiliyor | ✅ branch koruması açık |

---

## Göktürk Güvenlik Platformu

| Katman | Depo | Odak |
| :---: | :--- | :--- |
| **Çekirdek** | [**`gokturk-core`**](https://github.com/GokturkFK/gokturk-core) | Dondurulmuş `trap` sözleşmesi & belirlenimci `correlate` motoru |
| **P1** | [**`gokturk-deception-mesh`**](https://github.com/GokturkFK/gokturk-deception-mesh) | Kimlik kanaryaları, SSH sensörü, NATS JetStream & birleşik SOC paneli |
| **P2** | [**`gokkalkan`**](https://github.com/GokturkFK/gokkalkan) | Ajan tabanlı yapay zekâ güvenliği — MCP egress proxy & ajan honeypot |
| **P3** | **`gokzincir`** *(bu depo)* | İnsan-olmayan kimlik (NHI) yönetişimi & blast-radius grafiği |

---

<a id="-english"></a>

<details>
<summary><b>🇬🇧 English</b> — click to expand</summary>

<br/>

## Overview

**GÖKZİNCİR** (Pillar 3 of the Göktürk Security Platform) combines **Non-Human Identity (NHI) governance** with **stealth deception**:
- **Blast-Radius Graph:** Inventories service accounts, tokens, and machine identities (`nhi_identities`, `nhi_edges`) to compute true compromise reachability.
- **Stealth Decoy Seeding:** Automatically plants distribution-matched decoy NHIs after the first inventory pass. Decoy IDs are never returned by read APIs.
- **Zero-FP Detection:** Benign usage raises zero alerts; touching a decoy raises an instant `T1078.004` **Critical** alert via [`gokturk-core`](https://github.com/GokturkFK/gokturk-core) (83.5% test coverage in CI).

## Quickstart

```sh
cp deployments/docker/.env.example deployments/docker/.env
make docker-up          # Postgres :5434, NATS :4224/8224
./scripts/demo.sh
```

</details>

---

<div align="center">
  <sub><a href="https://github.com/GokturkFK">GokturkFK</a> · Güvenlik Çekirdeği: <a href="https://github.com/fetihcakmak">@fetihcakmak</a> · Platform/DevOps: <a href="https://github.com/uzunkubra50">@uzunkubra50</a></sub>
</div>
