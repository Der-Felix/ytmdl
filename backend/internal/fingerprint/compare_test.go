package fingerprint

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestComparisonRequiresInformativeAlignedOverlap(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	a := make([]uint32, 500)
	for i := range a {
		a[i] = random.Uint32()
	}
	b := append([]uint32{1, 2, 3, 4, 5}, a...)
	for i := range b {
		if i%4 == 0 {
			b[i] ^= 1
		}
	}
	if Similarity(a, b) < 0.98 {
		t.Fatal("codec noise and small offset should match")
	}
	unrelated := make([]uint32, 500)
	for i := range unrelated {
		unrelated[i] = random.Uint32()
	}
	if Similarity(a, unrelated) >= 0.92 {
		t.Fatal("unrelated audio must not match")
	}
	if Similarity(make([]uint32, 500), make([]uint32, 500)) != 0 || Similarity(a[:80], a[:80]) != 0 {
		t.Fatal("uninformative or short measurements must be inconclusive")
	}
	if !strings.HasPrefix(Buckets(a), "{") || len(strings.Split(strings.Trim(Buckets(a), "{}"), ",")) > 128 {
		t.Fatal("index must be bounded")
	}
}
