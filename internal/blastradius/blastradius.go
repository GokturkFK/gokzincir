// Package blastradius, GZ-A2: bir NHI ele geçirilirse ulaşılabilir düğüm
// kümesini ve buna karşılık gelen risk skorunu hesaplar.
//
// docs/DECISIONS.md Karar 3'te onaylanan tasarım: yönlü BFS, maksimum 5 adım
// derinlik, ziyaret kümesiyle döngü koruması, doğrusal normalize skor
// (0-1). Bilinçli olarak ML/sezgisel yok — deterministik, sıfır-FP tezi
// ancak böyle savunulabilir (PROJECT_PLAN.md böl. 7).
//
// Bu paket saf/DB'den bağımsızdır: kenar verisini bir Graph arayüzü
// arkasından alır. Gerçek Postgres implementasyonu (recursive CTE mi,
// uygulama katmanında tekrarlı sorgu mu) GZ0-3'ün mimari kararı, bu paketi
// etkilemez.
package blastradius

import (
	"context"
	"sort"
)

// MaxDepth, BFS'in duracağı en fazla adım derinliğidir (docs/DECISIONS.md
// Karar 3: sınırsız derinlik anlamsız büyük kümeler üretir).
const MaxDepth = 5

// Graph, blast-radius hesabı için gereken kenar verisine erişimi soyutlar.
// Neighbors, verilen düğümden YÖNLÜ olarak ulaşılabilen komşuları döner
// (docs/DECISIONS.md Karar 2: kenar "A, B'ye erişebilir" anlamına gelir).
type Graph interface {
	Neighbors(ctx context.Context, nodeID string) ([]string, error)
	// TotalNodes, normalize skor için envanterdeki toplam NHI sayısıdır.
	TotalNodes(ctx context.Context) (int, error)
}

// Result, bir kaynak düğümden hesaplanan blast-radius sonucudur.
type Result struct {
	SourceID string
	// Reachable, kaynağın kendisi hariç, MaxDepth içinde ulaşılan
	// düğümlerin kümesidir (sırasız; deterministik test için ayrıca bkz.
	// ReachableSorted).
	Reachable map[string]struct{}
	// Score, len(Reachable)/TotalNodes oranıdır, [0,1] aralığına
	// sıkıştırılır. Bu bir ALARM değildir — salt görünürlük/rapor
	// amaçlıdır (docs/DECISIONS.md Karar 3).
	Score float64
}

// ReachableSorted, Reachable kümesini deterministik (alfabetik) sırada
// döner — testlerde ve API çıktısında kararlı sıralama için.
func (r Result) ReachableSorted() []string {
	out := make([]string, 0, len(r.Reachable))
	for id := range r.Reachable {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Compute, sourceID'den başlayan yönlü BFS ile blast-radius'u hesaplar.
//
// Döngüler ziyaret edilen küme (visited) ile kesilir: bir düğüm en fazla
// bir kez işlenir, sonsuz genişleme olmaz. MaxDepth adımdan sonra BFS
// durur — o adımda kuyrukta bekleyen düğümler Reachable'a dahil edilmez
// (docs/DECISIONS.md: "sınırsız derinlik anlamsız büyük kümeler üretir").
func Compute(ctx context.Context, g Graph, sourceID string) (Result, error) {
	total, err := g.TotalNodes(ctx)
	if err != nil {
		return Result{}, err
	}

	visited := map[string]struct{}{sourceID: {}}
	reachable := make(map[string]struct{})

	frontier := []string{sourceID}
	for depth := 0; depth < MaxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, node := range frontier {
			neighbors, err := g.Neighbors(ctx, node)
			if err != nil {
				return Result{}, err
			}
			for _, n := range neighbors {
				if _, seen := visited[n]; seen {
					continue
				}
				visited[n] = struct{}{}
				reachable[n] = struct{}{}
				next = append(next, n)
			}
		}
		frontier = next
	}

	return Result{
		SourceID:  sourceID,
		Reachable: reachable,
		Score:     normalizeScore(len(reachable), total),
	}, nil
}

// normalizeScore, docs/DECISIONS.md Karar 3'teki formülü uygular:
// min(ulaşılan/toplam, 1.0). total<=0 durumunda (boş envanter) 0 döner —
// bölme hatası yerine güvenli varsayılan.
func normalizeScore(reached, total int) float64 {
	if total <= 0 {
		return 0
	}
	score := float64(reached) / float64(total)
	if score > 1.0 {
		return 1.0
	}
	return score
}
