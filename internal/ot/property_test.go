package ot

import (
	"encoding/json"
	"math/rand"
	"testing"
)

// Property tests: generate random documents and random operation pairs, and
// assert the algebraic guarantees the collaboration protocol depends on.

const propertyIterations = 2000

// alphabet mixes 1-byte, 2-byte, 3-byte and 4-byte UTF-8 code points so byte
// vs code-point confusion cannot slip through.
var alphabet = []rune("abcXYZ 0Ωé中文字🌍😀\n")

func randDoc(rng *rand.Rand, maxLen int) string {
	n := rng.Intn(maxLen + 1)
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(runes)
}

func randInsert(rng *rand.Rand) string {
	n := 1 + rng.Intn(5)
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(runes)
}

// randOp builds a random valid operation against doc.
func randOp(rng *rand.Rand, doc string) *Operation {
	op := New()
	remaining := len([]rune(doc))
	for remaining > 0 {
		n := 1 + rng.Intn(remaining)
		switch rng.Intn(3) {
		case 0:
			op.Retain(n)
			remaining -= n
		case 1:
			op.Delete(n)
			remaining -= n
		case 2:
			op.Insert(randInsert(rng))
		}
	}
	if rng.Intn(2) == 0 {
		op.Insert(randInsert(rng))
	}
	return op
}

// TP1 convergence: applying a then b' must equal applying b then a'.
func TestTransformConvergenceProperty(t *testing.T) {
	for seed := int64(0); seed < propertyIterations; seed++ {
		rng := rand.New(rand.NewSource(seed))
		doc := randDoc(rng, 40)
		a := randOp(rng, doc)
		b := randOp(rng, doc)
		a2, b2, err := Transform(a, b)
		if err != nil {
			t.Fatalf("seed %d: Transform: %v", seed, err)
		}
		docA, err := a.Apply(doc)
		if err != nil {
			t.Fatalf("seed %d: apply a: %v", seed, err)
		}
		docB, err := b.Apply(doc)
		if err != nil {
			t.Fatalf("seed %d: apply b: %v", seed, err)
		}
		viaA, err := b2.Apply(docA)
		if err != nil {
			t.Fatalf("seed %d: apply b' after a: %v", seed, err)
		}
		viaB, err := a2.Apply(docB)
		if err != nil {
			t.Fatalf("seed %d: apply a' after b: %v", seed, err)
		}
		if viaA != viaB {
			t.Fatalf("seed %d diverged:\n doc=%q\n a=%s\n b=%s\n viaA=%q\n viaB=%q", seed, doc, a, b, viaA, viaB)
		}
	}
}

// Compose equivalence: Apply(doc, Compose(a, b)) == Apply(Apply(doc, a), b).
func TestComposeEquivalenceProperty(t *testing.T) {
	for seed := int64(0); seed < propertyIterations; seed++ {
		rng := rand.New(rand.NewSource(seed))
		doc := randDoc(rng, 40)
		a := randOp(rng, doc)
		docA, err := a.Apply(doc)
		if err != nil {
			t.Fatalf("seed %d: apply a: %v", seed, err)
		}
		b := randOp(rng, docA)
		docAB, err := b.Apply(docA)
		if err != nil {
			t.Fatalf("seed %d: apply b: %v", seed, err)
		}
		ab, err := Compose(a, b)
		if err != nil {
			t.Fatalf("seed %d: Compose: %v", seed, err)
		}
		got, err := ab.Apply(doc)
		if err != nil {
			t.Fatalf("seed %d: apply composed: %v", seed, err)
		}
		if got != docAB {
			t.Fatalf("seed %d compose mismatch:\n doc=%q\n a=%s\n b=%s\n got=%q\n want=%q", seed, doc, a, b, got, docAB)
		}
		if ab.BaseLen() != a.BaseLen() || ab.TargetLen() != b.TargetLen() {
			t.Fatalf("seed %d: composed lengths base=%d target=%d, want base=%d target=%d",
				seed, ab.BaseLen(), ab.TargetLen(), a.BaseLen(), b.TargetLen())
		}
	}
}

// JSON roundtrip: marshal → unmarshal reproduces an identical operation.
func TestJSONRoundtripProperty(t *testing.T) {
	for seed := int64(0); seed < propertyIterations; seed++ {
		rng := rand.New(rand.NewSource(seed))
		doc := randDoc(rng, 40)
		op := randOp(rng, doc)
		data, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("seed %d: marshal: %v", seed, err)
		}
		var back Operation
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("seed %d: unmarshal %s: %v", seed, data, err)
		}
		if back.String() != op.String() || back.BaseLen() != op.BaseLen() || back.TargetLen() != op.TargetLen() {
			t.Fatalf("seed %d roundtrip mismatch: %s vs %s", seed, op, back.String())
		}
	}
}

// TransformIndex stays within the transformed document's bounds.
func TestTransformIndexBoundsProperty(t *testing.T) {
	for seed := int64(0); seed < propertyIterations; seed++ {
		rng := rand.New(rand.NewSource(seed))
		doc := randDoc(rng, 40)
		op := randOp(rng, doc)
		pos := 0
		if op.BaseLen() > 0 {
			pos = rng.Intn(op.BaseLen() + 1)
		}
		got := op.TransformIndex(pos)
		if got < 0 || got > op.TargetLen() {
			t.Fatalf("seed %d: TransformIndex(%d) = %d out of [0, %d] for %s", seed, pos, got, op.TargetLen(), op)
		}
	}
}
