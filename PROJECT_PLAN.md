# GÖKZİNCİR — NHI Governance & Blast-Radius Graph · Görev Planı

> İki kişilik ekip için hazırlanmış, board'a doğrudan taşınabilir görev dökümü.
> **Cyber** (`@fetihcakmak`) = güvenlik çekirdeği: NHI risk modeli, blast-radius
> algoritması, sahte NHI (deception) tasarımı, tehdit modeli.
> **DevOps** (`@uzunkubra50`) = platform, teslimat, ops + envanter toplama
> hattı, graph deposu, API, panel entegrasyonu, CI/CD.
>
> Bu, Göktürk platformunun **üçüncü** ürünüdür. Öncekiler:
> [GÖKTÜRK](https://github.com/GokturkFK/gokturk-deception-mesh) (P1, deception mesh, v0.1 ✅) ve
> [GÖKKALKAN](https://github.com/GokturkFK/gokkalkan) (P2, agentic AI runtime security, v0.1 ✅).
>
> Platform kuralı: **aynı anda tek aktif ürün** — bir ürün kendi DoD'sine
> ulaşmadan sonraki başlatılmaz. G2 kapısı (GÖKKALKAN DoD) geçildi, bu ürün
> artık **aktif**.
>
> Ortak omurga [gokturk-core](https://github.com/GokturkFK/gokturk-core) `v0.1.0`
> import edilir, **kopyalanmaz** (`trap.Provider`, `trap.TripEvent`,
> `correlate.Alert`, `correlate.Evaluate`).

---

## 1. Milestone hedefi (tek cümle)

Bir saldırgan, ele geçirdiği bir servis hesabıyla (NHI) yanal hareket etmeye
kalkar → **blast-radius grafiği** o kimliğin gerçekte nereye erişebildiğini
gösterir; ekilmiş **sahte bir NHI**'ya dokunduğu anda panelde **Critical**
alarm belirir — meşru servis-hesabı kullanımı ise **hiçbir alarm üretmez**
(sıfır-FP, GÖKTÜRK/GÖKKALKAN ile aynı tez, yeni yüzey).

---

## 2. Kapsam (v0.1)

**Dahil:**
- **NHI envanteri**: servis hesapları / token'lar / makine kimlikleri tek bir
  kanonik modelde toplanır (v0.1'de tek bir kaynak tipi yeter).
- **İlişki grafiği**: "hangi kimlik neye erişiyor" kenarları.
- **Blast-radius hesabı**: bir kimlik ele geçerse ulaşılabilir düğüm kümesi
  ve bunun bir risk skoruna indirgenmesi.
- **Sahte NHI ekimi**: `gokturk-core/trap.Provider`'ın yeni bir uygulaması —
  gerçekmiş gibi duran, dokunulunca `TripEvent` üreten bir servis hesabı.
- Tuzak tetiklenince → `correlate.Evaluate` → alarm → **aynı SOC paneli**.

**Hariç (bilinçli ertelendi):**
- Gerçek bulut sağlayıcı (AWS/Azure/GCP) IAM entegrasyonu → v0.2. v0.1'de
  envanter, taşınabilir tek bir kaynak tipinden beslenir; amaç graph ve
  blast-radius mantığını doğrulamak, sağlayıcı SDK'sı yazmak değil.
- Ayrı bir graph veritabanı (Neo4j vb.) → **Sprint 0 kararı**, bkz. böl. 3.
  Roadmap "ağır altyapı" diyor ama v0.1 için gerekliliği kanıtlanmadı.
- Otomatik remediation (kimliği kes/rotate et) → GÖKKALKAN'ın enforcement
  deseni burada da uygulanabilir ama v0.1 kapsamı **görünürlük**.
- Compliance raporlama eşlemesi → v0.2.

---

## 3. Önce sözleşmeleri doğrula (Sprint 0) — **BLOCKER**

GÖKTÜRK ve GÖKKALKAN'da olduğu gibi, kod yazımından önce donması gereken
kararlar var. Bunlar **Cyber'in** kararı; DevOps şemayı önceden dondurmaz
(GÖKKALKAN'da `migrations/00001_init.sql` bilinçli olarak sadece
`trip_events`/`alerts` içeriyordu, agent tabloları Sprint 0 sonrası eklendi —
aynı disiplin).

Netleşmesi gerekenler:

1. **NHI veri modeli.** Bir NHI'yı ne tanımlar? (tip, sahip, oluşturulma,
   son kullanım, kapsam/scope, sır referansı). Sırların kendisi **asla**
   tutulmaz — GÖKTÜRK'teki `traps.secret_hash` deseni geçerli.
2. **Kenar (edge) semantiği.** Graf'ta bir kenar neyi ifade eder: erişim mi,
   sahiplik mi, üstlenme (assume-role) mi? Blast-radius'un anlamı buna bağlı.
   Yönlü mü, ağırlıklı mı?
3. **Blast-radius tanımı.** Ulaşılabilirlik kaç adım derinlikte hesaplanır,
   döngüler nasıl ele alınır, skor neye göre normalize edilir?
4. **Teknik eşlemesi** — `correlate.Evaluate(trips, technique)`'e hangi kod
   geçilecek? Sahte NHI kullanımı klasik ATT&CK'te karşılığı olan bir
   davranış (`T1078` *Valid Accounts* güçlü aday, GÖKTÜRK'te de o kullanıldı;
   `T1078.004` *Cloud Accounts* alt tekniği daha spesifik olabilir). ATLAS
   değil ATT&CK olması bekleniyor — bu bir AI/agent yüzeyi değil.
   **ID'ler resmî veri deposundan doğrulanmalı**, hafızadan/blogdan değil.
5. **Sahte NHI'nın inandırıcılığı.** Gerçek envanterin içinde nasıl
   ayırt edilemez durur (GK-B1'deki "tespit-edilemezlik" işinin muadili).

> Bu kararlar `docs/DECISIONS.md`'ye yazılır ve **Cyber onaylayınca** kesinleşir
> (GÖKKALKAN'da GK-S0 böyle yürüdü). Onaylanmadan GZ-A/GZ-B kod yazımı başlamaz.

---

## 4. Görevler — CYBER (güvenlik çekirdeği)

> Sahiplik kuralı GÖKTÜRK/GÖKKALKAN ile birebir aynı: `@fetihcakmak` yalnızca
> risk/tespit mantığını ve tehdit modelini yürütür; platform tarafı DevOps'ta.

### EPIC GZ-A — NHI risk modeli
- **GZ-A1 · NHI veri modeli + kenar semantiği** — Sprint 0 kararının kendisi;
  `docs/DECISIONS.md`'ye yazılır.
  - *AC:* Bir NHI'yı ve iki NHI arasındaki ilişkiyi tanımlayan alan kümesi
    netleşmiş, gerekçeleriyle yazılmış olur.
- **GZ-A2 · Blast-radius algoritması** — bir düğümden ulaşılabilir kümenin
  hesabı + risk skoru.
  - *AC:* Bilinen bir test grafiğinde beklenen yarıçapı deterministik olarak
    üretir; ML/sezgisel yok (sıfır-FP disiplini).
  - *Dep:* GZ-A1

### EPIC GZ-B — Sahte NHI (deception)
- **GZ-B1 · Sahte NHI provider'ı** — `gokturk-core/trap.Provider`'ı uygulayan
  yeni tuzak türü: gerçek gibi duran ama kullanılınca `TripEvent` üreten bir
  servis hesabı.
  - *AC:* Sahte NHI kullanıldığında doğru `TripEvent` düşer; meşru NHI
    kullanımı hiçbir şey tetiklemez (sıfır-FP).
  - *Dep:* `gokturk-core/trap.Provider`, GZ-A1

### EPIC GZ-F — Tehdit modeli
- **GZ-F1 · Tehdit modeli & framework eşlemesi** — NHI'ye özgü saldırı
  senaryoları, ATT&CK eşlemesi, README mimari bölümü, demo GIF.
  - *AC:* README'de mimari diyagram + `docs/THREAT_MODEL.md` + demo GIF
    (GÖKTÜRK APP-11 / GÖKKALKAN GK-F1 muadili).

---

## 5. Görevler — DEVOPS (platform/teslimat/ops + hat)

> *Sahip:* Bu bölümdeki tüm görevler `@uzunkubra50`.

### EPIC GZ-C — Envanter & graph hattı
- **GZ0-1 · Proje iskeleti & config** — `cmd/gokzincir`, ortam değişkeni
  tabanlı config, graceful shutdown, `/healthz`. **(bu commit'te tamamlandı)**
  - *AC:* `go run ./cmd/gokzincir` boot oluyor, `/healthz` 200 dönüyor.
- **GZ0-2 · Envanter toplama hattı** — NHI kayıtlarını kanonik modele
  dönüştürüp kalıcı yazan hat.
  - *AC:* Bir kaynak dosyasından/uçtan gelen kayıtlar idempotent şekilde
    yazılıyor; tekrar çalıştırma çift kayıt üretmiyor.
  - *Dep:* GZ-A1 (model)
- **GZ0-3 · Graph deposu + sorgu katmanı** — kenarların kalıcı hali ve
  ulaşılabilirlik sorgusunun çalıştığı yer.
  - *AC:* GZ-A2'nin algoritması gerçek veri üzerinde çalışıyor; sorgu
    gerçek Postgres'e karşı doğrulanmış.
  - *Dep:* GZ-A1, GZ-A2

### EPIC GZ-D — Korelasyon & panel
- **GZ0-4 · Korelasyon wiring** — GZ-B'nin ürettiği `TripEvent`'i
  `correlate.Evaluate`'e besle, alarmı kalıcı yaz.
  - *AC:* Sahte NHI tetiklenince ≤5 sn içinde panelde alarm.
  - *Dep:* GZ-B1
- **GZ0-5 · Okuma API'si** — `GET /api/v1/alerts` (GÖKTÜRK/GÖKKALKAN ile
  **aynı** sözleşme) + NHI/blast-radius uçları.
  - *AC:* Panel `ALERT_SOURCES`'a eklenince üçüncü kaynak olarak görünüyor,
    panelde kod değişikliği gerekmiyor.
  - *Dep:* GZ0-4

### EPIC GZ-G — Repo & CI/CD
- **GZ0-6 · Repo hijyeni + CI** — build/vet/test/lint/Trivy, branch
  protection, squash-only. **(bu commit'te tamamlandı)**
- **GZ0-7 · DB migration iskeleti** — `trip_events`/`alerts` şeması
  (gokturk-core ile aynı şekil). NHI/graph tabloları GZ-A1 netleşince
  eklenir — DevOps bunu önceden dondurmaz. **(iskelet bu commit'te)**
  - *AC:* `make migrate-up/down` çalışıyor.

---

## 6. Definition of Done (v0.1)

Aşağıdakilerin **hepsi** doğruysa GÖKZİNCİR v0.1 biter:

1. NHI envanteri doldurulabiliyor ve ilişki grafiği kurulabiliyor.
2. Bir NHI için blast-radius hesaplanıp API'den okunabiliyor.
3. Ekilmiş sahte bir NHI kullanılınca ≤5 sn içinde panelde alarm belirir.
4. Meşru NHI kullanımı → hiçbir alarm (sıfır-FP).
5. `go test ./... -race` yeşil, çekirdek kapsam ≥ %70.
6. README: mimari diyagram + tehdit modeli + demo GIF.
7. CI PR'ı bloklayabiliyor.

---

## 7. Riskler & kilit noktalar

- **Kontrat kayması:** `gokturk-core`'daki `TripEvent`/`Alert` şekli değişirse
  üç ürün birden kırılır → değişiklik ancak version bump ile.
- **Kapsam sürünmesi:** roadmap'in P3 DoD'si kilitli; otomatik remediation
  (GÖKKALKAN'ın alanı) veya çok-sensörlü korelasyon servisi (P4) buraya
  sızmaz.
- **Erken ağır altyapı:** Ayrı bir graph DB v0.1 için gerekli olmayabilir;
  Postgres recursive CTE yeterliyse onunla başlanır. Gereklilik
  **ölçülmeden** bağımlılık eklenmez.
- **Sıfır-FP korunmalı:** Blast-radius bir risk skoru üretir; bu skor
  **alarm** üretmemeli. Alarm yalnızca bir tuzağa fiilen dokunulduğunda
  çıkar — GÖKTÜRK'teki "deterministik korelasyon, ML yok" ilkesiyle aynı.
