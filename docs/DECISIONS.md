# GZ-S0 · Sprint 0 tasarım kararları

> **Durum:** öneri — @fetihcakmak onaylayınca kesinleşir (issue #1).
> DevOps tarafı bloke olmasın diye somut bir başlangıç noktası olarak yazıldı;
> nihai söz güvenlik çekirdeğinin sahibinde. Değiştirirsen bu dosyayı güncelle,
> `internal/`'daki kod ve `migrations/` ona göre şekillenir.

GÖKZİNCİR'de wire contract zaten donuk (`gokturk-core` v0.1.0). Geriye bu
ürüne özgü beş karar kalıyor (PROJECT_PLAN.md böl. 3).

---

## Karar 1 — NHI veri modeli

### Öneri: tek bir kanonik `nhi_identities` modeli

v0.1 kapsamı tek kaynak tipi (PROJECT_PLAN.md böl. 2: gerçek bulut IAM
entegrasyonu v0.2'ye ertelendi). Model, kaynak-bağımsız kalacak şekilde
tasarlandı — v0.2'de AWS/Azure/GCP IAM'den beslenince alan kümesi
değişmeyecek, yalnızca `source_type` çeşitlenecek.

| Alan | Tip | Gerekçe |
|---|---|---|
| `type` | text | "service_account" / "token" / "machine_identity" — v0.1'de üçü de aynı tabloda, ayrım sadece etiket |
| `owner` | text | insan veya sistem sahibi (denetim için) |
| `created_at` | timestamptz | envanterin kendisi ne zaman öğrendi (kaynaktaki gerçek oluşturulma zamanı değil — o kaynağa göre değişir, v0.1'de garanti edilemez) |
| `last_used_at` | timestamptz, nullable | blast-radius skorlamasında "aktif mi" sinyali |
| `scope` | text | erişim kapsamının serbest-metin özeti (v0.1'de yapılandırılmış izin listesi değil — GZ-A2'nin blast-radius hesaplaması kenarlara bakar, bu alana değil) |
| `secret_ref_hash` | text, nullable | **sır asla ham tutulmaz** — GÖKTÜRK'teki `traps.secret_hash` deseninin aynısı, HMAC özeti |
| `is_decoy` | boolean | GZ-B1'in ektiği sahte NHI'ları gerçek envanterden ayırt etmek için — ama bu alan **hiçbir API/panel çıktısına sızmaz** (Karar 5'e bkz.), yalnızca iç sorgu/temizlik amaçlı |

**Neden `is_decoy` var ama "inandırıcılık" ihlal olmuyor?** Alan DB'de var,
ama hiçbir okuma yolunda (GZ0-5 API'si) döndürülmez — GÖKTÜRK'teki
`SecretHash string \`json:"-"\`` deseninin aynısı: Go tarafında struct alanı
var, JSON'a hiç çıkmıyor.

---

## Karar 2 — Kenar (edge) semantiği

### Öneri: tek kenar tipi, yönlü, "erişim" anlamına gelir

`nhi_edges (from_id, to_id, relation)`:

- **Tek anlam:** kenar `A → B`, "A, B'ye erişebilir" demektir. Sahiplik ve
  role-assumption ayrı kenar tipleri olarak **eklenmedi** — v0.1'in tek
  sorusu "ele geçirilen kimlik nereye ulaşır", o da saf erişim grafiğiyle
  cevaplanır. Sahiplik/devir semantiği v0.2'ye (gerçek IAM entegrasyonuyla
  birlikte, çünkü o zaman assume-role zincirleri gerçek veri kaynağından
  gelecek).
- **Yönlü:** zorunlu — "A, B'ye erişebilir" ile "B, A'ya erişebilir" farklı
  risk anlamına gelir (blast-radius yönlü bir erişilebilirlik BFS'idir).
- **Ağırlıksız (v0.1):** ağırlık (örn. "ne kadar güçlü erişim") skorlama
  karmaşıklığını artırır, kanıtlanmadan eklenmiyor (PROJECT_PLAN.md böl. 7
  "erken ağır altyapı" riskiyle aynı disiplin). Blast-radius v0.1'de saf
  ulaşılabilirlik (BFS derinliği), ağırlıklı yol değil.

```sql
CREATE TABLE nhi_edges (
    from_id  uuid NOT NULL REFERENCES nhi_identities(id),
    to_id    uuid NOT NULL REFERENCES nhi_identities(id),
    relation text NOT NULL DEFAULT 'access',
    PRIMARY KEY (from_id, to_id)
);
```

`relation` şimdiden bir kolon olarak var (tek değer alsa da) — v0.2'de
sahiplik/devir eklenince migration'da yeni bir tablo gerekmez, sadece yeni
değerler ve GZ-A2'nin BFS'inin `relation`'a göre filtrelenmesi.

---

## Karar 3 — Blast-radius tanımı

### Öneri: sınırlı-derinlik BFS + doğrusal normalize skor

- **Ulaşılabilirlik:** `from_id`'den başlayan yönlü BFS, **maksimum 5 adım**
  derinlikte durur. Sınırsız derinlik gerçek dünyada anlamsız büyük
  kümeler üretir (GÖKTÜRK'teki "sıfır-FP" tezini blast-radius tarafında
  karşılığı: aşırı geniş bir "risk" herkesin göz ardı ettiği bir alarm
  yorgunluğuna döner).
- **Döngüler:** BFS ziyaret edilen düğüm kümesi tutar (`visited` set), bir
  düğüm bir kez sayılır — döngü sonsuz genişlemeye yol açmaz, sadece
  erken durur.
- **Skor normalize:** `min(ulaşılan_düğüm_sayısı / toplam_nhi_sayısı, 1.0)`
  — 0 ile 1 arasında, kaynak envanterin büyüklüğünden bağımsız
  karşılaştırılabilir bir oran. Ağırlıklı/olasılıksal bir model
  **kullanılmıyor** (ML yok disiplini, PROJECT_PLAN.md böl. 7).
- **Bu bir alarm DEĞİL.** Blast-radius skoru salt görünürlük/rapor
  amaçlı (DoD madde 2: "API'den okunabiliyor"); alarm yalnızca Karar 5'teki
  sahte NHI'ya fiilen dokunulduğunda üretilir (DoD madde 3-4).

### Bu kararın DEĞİŞTİRİLMESİ gereken yer (GZ0-3'ün alanı)

Postgres recursive CTE mi yoksa uygulama katmanında BFS mi — bu bir
performans/mimari kararı, DevOps'un (GZ0-3) alanı. Yukarıdaki *semantik*
tanım (max 5 adım, ziyaret kümesi, doğrusal normalize) hangi
implementasyonla yapılırsa yapılsın sabit kalmalı.

---

## Karar 4 — Teknik eşlemesi (`correlate.Evaluate(trips, technique)`)

### ATT&CK, ATLAS değil

GÖKKALKAN'da ATLAS kullanıldı çünkü orada MCP/agent'a özgü bir saldırı
yüzeyi vardı. GÖKZİNCİR'de sahte bir servis hesabının kullanılması klasik
kimlik hırsızlığı — GÖKTÜRK'ün (P1) zaten kullandığı ailenin aynısı.

### Öneri: `T1078.004` (GÖKTÜRK'ün düz `T1078`'inden daha spesifik)

MITRE ATT&CK Enterprise resmi sitesinden (`attack.mitre.org`) doğrulandı:

| Teknik | Tam ad | Neden bu, düz `T1078` değil |
|---|---|---|
| **`T1078.004`** | Valid Accounts: Cloud Accounts | Tanımı birebir örtüşüyor: *"Cloud accounts are those created and configured by an organization for use by users, remote support, **services**, or for administration of resources..."* GÖKZİNCİR'in NHI'ları (servis hesabı/token/makine kimliği) tam bu kategori. GÖKTÜRK'teki düz `T1078` insan kullanıcı kimlik bilgisi hırsızlığıydı (SSH); burada envanterin kendisi servis hesaplarına özgü, alt teknik daha isabetli. |

**Neden bir OWASP/Agentic eşlemesi yok?** GÖKZİNCİR bir AI agent yüzeyi
değil — klasik kimlik/erişim yönetimi tehdidi. GZ-F1 tehdit modelinde NIST
NHI yönetim rehberi (varsa) veya CSA Non-Human Identity Security çerçevesi
gibi NHI'ye özgü bir kaynakla eşleme yapılabilir; bu, alarmın `technique`
alanını etkilemez, yalnızca dokümantasyon.

### Kodda karşılığı

```go
const TechniqueFakeNHIUsage = "T1078.004" // sahte NHI kullanildi

alerts := correlate.Evaluate(trips, TechniqueFakeNHIUsage)
```

Panelin teknik sütunu zaten `^T\d{4}(\.\d{3})?$` regex'iyle ATT&CK linki
üretiyor (GÖKKALKAN'ın ATLAS'ı için ayrı bir dal gerektirmişti — burada
buna gerek yok, format doğrudan uyuyor).

---

## Karar 5 — Sahte NHI'nın inandırıcılığı

### Öneri: GK-B1 ile aynı desen, `is_decoy` API'ye hiç çıkmaz

- Sahte NHI, `nhi_identities` tablosuna **gerçek bir kayıt gibi** yazılır —
  ayrı bir "decoy" tablosu yok (GÖKKALKAN'daki `honeypot_tools`'un aksine,
  çünkü burada tuzak "ayrı bir tool" değil, "envanterin içinde bir satır"
  olmalı — ayrı tabloda tutmak sorgu/API katmanında filtrelemeyi
  unutma riskini `is_decoy` kolonundan daha kırılgan hale getirirdi).
- `is_decoy = true` olan satırlar GZ0-5'in **hiçbir** okuma ucunda
  görünmez (ne `/api/v1/nhi`, ne blast-radius sorgu sonucunda düğüm
  listesinde) — yalnızca GZ-B1'in kendi `Decoder`'ı bu alana bakar.
- Sahte NHI'nın `owner`/`scope` alanları gerçek envanterdeki dağılıma
  **istatistiksel olarak** benzemeli (örn. gerçek envanterde en sık görülen
  `owner` değerlerinden biri seçilir) — bu, GZ-B1'in kod tasarımı, kolon
  yapısı değil.

### Bu kararın DEĞİŞTİRİLMESİ gereken yer (GZ-B1'in alanı)

`scope`/`owner` alanlarının nasıl "istatistiksel olarak inandırıcı"
üretileceği — kolon yapısı burada donuk, içerik GZ-B1'in işi (GÖKKALKAN'daki
`honeypot_tools.description`'ın "inandırıcılığı" ile aynı ayrım).

---

## Referanslar

- MITRE ATT&CK T1078: https://attack.mitre.org/techniques/T1078/
- MITRE ATT&CK T1078.004: https://attack.mitre.org/techniques/T1078/004/
- gokturk-core `trap`/`correlate` sözleşmesi: https://github.com/GokturkFK/gokturk-core
