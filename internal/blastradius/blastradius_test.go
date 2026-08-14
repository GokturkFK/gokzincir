package blastradius

import (
	"context"
	"testing"
)

// fakeGraph, sabit bir komşuluk listesiyle Graph arayüzünü karşılar.
type fakeGraph struct {
	edges map[string][]string
	total int
}

func (g fakeGraph) Neighbors(_ context.Context, nodeID string) ([]string, error) {
	return g.edges[nodeID], nil
}

func (g fakeGraph) TotalNodes(_ context.Context) (int, error) {
	return g.total, nil
}

// Bilinen test grafiği: A -> B -> C -> D -> E -> F -> G (zincir, 6 kenar).
// MaxDepth=5 olduğu için A'dan başlarsak B,C,D,E,F ulaşılır (5 adım),
// G (6. adım) ulaşılmaz — issue #2 AC'sinin ("bilinen grafikte deterministik
// yarıçap") doğrudan karşılığı.
func chainGraph() fakeGraph {
	return fakeGraph{
		edges: map[string][]string{
			"A": {"B"},
			"B": {"C"},
			"C": {"D"},
			"D": {"E"},
			"E": {"F"},
			"F": {"G"},
		},
		total: 7,
	}
}

func TestCompute_ChainRespectsMaxDepth(t *testing.T) {
	res, err := Compute(context.Background(), chainGraph(), "A")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"B", "C", "D", "E", "F"}
	got := res.ReachableSorted()
	if len(got) != len(want) {
		t.Fatalf("Reachable = %v, istenen %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("Reachable[%d] = %q, istenen %q", i, got[i], id)
		}
	}
	if _, ok := res.Reachable["G"]; ok {
		t.Error("G, MaxDepth disinda oldugu icin ulasilmamis olmali")
	}
}

func TestCompute_ScoreIsLinearNormalized(t *testing.T) {
	res, err := Compute(context.Background(), chainGraph(), "A")
	if err != nil {
		t.Fatal(err)
	}
	// 5 ulasilan / 7 toplam
	want := 5.0 / 7.0
	if res.Score != want {
		t.Errorf("Score = %v, istenen %v", res.Score, want)
	}
}

// Dongu: A -> B -> C -> A. BFS sonsuz genislemeden durmali, her dugum
// bir kez sayilmali.
func TestCompute_CycleDoesNotLoop(t *testing.T) {
	g := fakeGraph{
		edges: map[string][]string{
			"A": {"B"},
			"B": {"C"},
			"C": {"A"},
		},
		total: 3,
	}

	res, err := Compute(context.Background(), g, "A")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"B", "C"}
	got := res.ReachableSorted()
	if len(got) != len(want) {
		t.Fatalf("Reachable = %v, istenen %v (dongu sonsuz genisledi mi?)", got, want)
	}
}

func TestCompute_NoNeighborsEmptyResult(t *testing.T) {
	g := fakeGraph{edges: map[string][]string{}, total: 10}

	res, err := Compute(context.Background(), g, "isolated")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reachable) != 0 {
		t.Errorf("izole dugum icin bos kume bekleniyordu, geldi: %v", res.Reachable)
	}
	if res.Score != 0 {
		t.Errorf("Score = %v, istenen 0", res.Score)
	}
}

func TestCompute_EmptyInventoryScoreIsZeroNotNaN(t *testing.T) {
	g := fakeGraph{edges: map[string][]string{"A": {"B"}}, total: 0}

	res, err := Compute(context.Background(), g, "A")
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != 0 {
		t.Errorf("total=0 icin Score=0 bekleniyordu (bolme hatasi yerine), geldi: %v", res.Score)
	}
}

// Skor asla 1.0'i gecmemeli (ornegin total, gercek ulasilabilir kumeden
// daha kucuk sayilirsa - tutarsiz veri durumunda bile).
func TestCompute_ScoreCappedAtOne(t *testing.T) {
	g := fakeGraph{
		edges: map[string][]string{"A": {"B", "C", "D"}},
		total: 2, // reachable (3) > total (2) - tutarsiz ama olabilir senaryo
	}

	res, err := Compute(context.Background(), g, "A")
	if err != nil {
		t.Fatal(err)
	}
	if res.Score > 1.0 {
		t.Errorf("Score 1.0'i gecti: %v", res.Score)
	}
}

var _ Graph = fakeGraph{}
