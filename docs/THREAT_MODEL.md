# GÖKZİNCİR — Tehdit Modeli

> GZ-F1 (issue #4). GÖKTÜRK'teki APP-11 / GÖKKALKAN'daki GK-F1'in muadili:
> NHI'ye özgü saldırı senaryoları, MITRE ATT&CK eşlemesi + kısa bir STRIDE
> tablosu. Teknik kod kararları için bkz. [docs/DECISIONS.md](DECISIONS.md);
> mimari diyagram için [README.md](../README.md#mimari).

## Kapsam

GÖKZİNCİR, insan-olmayan kimliklerin (servis hesabı / token / makine
kimliği) **envanterini** ve **erişim grafiğini** tutar, bir kimlik ele
geçirilirse ulaşılabilecek yüzeyi (blast-radius) görünür kılar. Tehdit
modeli iki senaryoya odaklanır:

1. Bir saldırgan, meşru bir NHI'nin kimlik bilgilerini ele geçirir ve onunla
   **yanal hareket** etmeye çalışır — blast-radius grafiği bu hareketin
   *nereye kadar* mümkün olduğunu gösterir (görünürlük, alarm değil).
2. Saldırgan, ekilmiş **sahte bir NHI**'yı gerçek sanıp kullanır — bu an,
   deterministik ve sıfır-FP bir `Critical` alarma dönüşür.

Kapsam dışı (v0.1, bkz. PROJECT_PLAN.md böl. 2): gerçek bulut IAM
entegrasyonu, otomatik remediation (kimlik kesme/rotate), compliance
raporlama eşlemesi, ayrı graph veritabanı.

## MITRE ATT&CK Eşlemesi

GÖKKALKAN'ın aksine burada ATLAS değil **ATT&CK** kullanılır —
`docs/DECISIONS.md` Karar 4'te gerekçelendirildiği gibi, sahte bir servis
hesabının kullanılması bir AI/agent yüzeyi değil, klasik kimlik hırsızlığı.

| Teknik | Karşılığı | GÖKZİNCİR kontrolü |
|---|---|---|
| **T1078.004** — Valid Accounts: Cloud Accounts | Saldırgan, ele geçirdiği (veya sahte sanıp kullandığı) bir servis hesabıyla erişim sağlar | `internal/nhitrap.Decoder` — sahte NHI kullanımı tespit edilince `TripEvent` üretir → `correlate.Evaluate(trips, "T1078.004")` |
| **T1087.004** *(ilişkili, v0.1 kapsamı dışı gözlem)* — Account Discovery: Cloud Account | Saldırgan, ele geçirdiği kimlikle envanterdeki diğer kimlikleri keşfetmeye çalışır | Blast-radius grafiği bu keşfin *sonucunu* modelliyor (hangi düğümlere ulaşılabilir), keşif eylemini kendisi gözlemlemiyor — v0.2 adayı |

### CSA Non-Human Identity Security çerçevesiyle ilişki

Cloud Security Alliance'ın NHI risk kategorilerinden GÖKZİNCİR'in
kapsadıkları:

| CSA NHI riski | GÖKZİNCİR karşılığı |
|---|---|
| Aşırı ayrıcalıklı / gölgede kalmış (shadow) NHI'lar | `nhi_edges` grafiği + blast-radius skoru — hangi NHI'nin erişimi orantısız büyük, görünür hale gelir |
| NHI ele geçirilmesinin tespit edilememesi | `internal/nhitrap` — ekilmiş sahte NHI, meşru kullanım örüntüsüyle ayırt edilemez durur (`docs/DECISIONS.md` Karar 5), dokunulunca alarm |
| Sır/kimlik bilgisi düz metin saklanması | `nhi_identities.secret_ref_hash` — sır asla ham tutulmaz (GÖKTÜRK `traps.secret_hash` deseni) |

**Bilinçli olarak kapsam dışı:** gerçek zamanlı IAM policy analizi,
otomatik ayrıcalık daraltma (least-privilege remediation) — bunlar v0.2
roadmap'i, v0.1'in hedefi salt görünürlük + tuzak.

## STRIDE Tablosu

| Kategori | Tehdit | Etkilenen bileşen | Kontrol |
|---|---|---|---|
| **S**poofing | Saldırganın ele geçirdiği kimlik bilgileriyle meşru bir NHI gibi davranması | `internal/nhitrap` | Sahte NHI'nın kendisi bu tehdidi tersine çevirir — gerçek göründüğü için saldırgan onu meşru sanıp kullanır, kullanım anı tespit noktasıdır |
| **T**ampering | `nhi_edges` grafiğinin çalışma zamanında değiştirilmesi (sahte kenar ekleyip blast-radius sonucunu manipüle etme) | `internal/blastradius`, graph deposu (GZ0-3) | Grafik her sorguda kaynaktan (Postgres) yeniden okunur, önbelleklenmiş/manipüle edilebilir bir ara temsil yok |
| **R**epudiation | Bir NHI'nin kullanıldığının inkâr edilmesi | `gokturk-core/trap.TripEvent` | `TripEvent.ObservedAt` + `Raw` (ham gözlem) — GÖKKALKAN'daki imzalı receipt deseni burada da uygulanabilir (v0.2 adayı, v0.1'de yok) |
| **I**nformation Disclosure | `is_decoy` alanının veya `secret_ref_hash`'in API'den sızması | `internal/nhitrap`, GZ0-5 okuma API'si | `is_decoy` hiçbir okuma ucuna çıkmaz (`docs/DECISIONS.md` Karar 5); sır yalnızca HMAC özeti olarak tutulur |
| **D**enial of Service | Blast-radius sorgusunun çok büyük/döngülü bir grafikte kilitlenmesi | `internal/blastradius.Compute` | `MaxDepth=5` + ziyaret kümesiyle döngü koruması — sınırsız BFS genişlemesi yapısal olarak imkânsız |
| **E**levation of Privilege | Sahte NHI'nin kendisinin gerçek bir ayrıcalık taşıması (tuzak, gerçek riske dönüşür) | `internal/nhitrap.Provider` | `Artifacts` boş döner — sahte NHI'ya hiçbir gerçek secret/yetki verilmez, yalnızca envanterde "var" görünür |

## Bilinçli Tasarım Kararları (sıfır-FP disiplini)

- **ML/anomali tespiti yok.** Blast-radius skoru saf BFS ulaşılabilirlik
  oranı; sahte NHI tespiti saf kimlik eşleşmesi (`nhi_id` envanterde var mı
  ve `is_decoy=true` mu). Olasılıksal bir model bu ikisinden hiçbirinde yok
  — PROJECT_PLAN.md böl. 7 "sıfır-FP ancak deterministik kurallarla
  savunulabilir" ilkesi.
- **Blast-radius bir alarm değil.** `docs/DECISIONS.md` Karar 3'te
  vurgulandığı gibi, skor salt görünürlük amaçlı. Alarm yalnızca sahte bir
  NHI'ya fiilen dokunulduğunda üretilir — geniş bir blast-radius'un kendisi
  "olay" sayılıp alarm yorgunluğuna yol açmaz.
- **Emin olunmayan durumda reddet / sızdırma.** `internal/nhitrap.Decoder`,
  envanterde karşılığı olmayan bir kullanım için `trap.ErrNotATrip` döner
  (event üretmez) — GÖKTÜRK/GÖKKALKAN ile aynı disiplin, meşru kullanım
  hiçbir zaman gürültüye dönüşmez.

## Bilinen Sınırlar / Gelecek İş

- Demo GIF (issue #4'ün üçüncü teslimatı) GZ0-4 (korelasyon wiring) ve
  GZ0-5 (okuma API'si) tamamlanana kadar eklenemez — `blastradius` ve
  `nhitrap` şu an ayrı Go paketleri, tek bir çalışan binary'de henüz
  birbirine bağlı değil (GÖKKALKAN'da GK-F1'in izlediği aynı sıra: önce
  tehdit modeli, wiring bitince demo).
- İmzalı action receipt (GÖKKALKAN GKO-3 muadili) v0.1 kapsamında yok —
  Repudiation satırında not edildi, v0.2 adayı.
- Gerçek bulut IAM entegrasyonu olmadığı için `T1087.004` (Account
  Discovery) yalnızca dolaylı olarak (blast-radius sonucu üzerinden)
  modelleniyor, doğrudan gözlemlenmiyor.
