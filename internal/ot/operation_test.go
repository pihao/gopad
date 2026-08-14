package ot

import (
	"encoding/json"
	"testing"
)

func mustApply(t *testing.T, op *Operation, doc string) string {
	t.Helper()
	out, err := op.Apply(doc)
	if err != nil {
		t.Fatalf("Apply(%s, %q): %v", op, doc, err)
	}
	return out
}

func TestApplyBasic(t *testing.T) {
	op := New().Retain(5).Insert(" cruel").Retain(7)
	got := mustApply(t, op, "hello world!")
	if got != "hello cruel world!" {
		t.Errorf("got %q", got)
	}
}

func TestApplyDelete(t *testing.T) {
	op := New().Retain(5).Delete(6).Insert(" gopad")
	got := mustApply(t, op, "hello world")
	if got != "hello gopad" {
		t.Errorf("got %q", got)
	}
}

func TestApplyUnicode(t *testing.T) {
	// Lengths count code points: "héllo" is 5, "🌍" is 1, "世界" is 2.
	op := New().Retain(5).Delete(1).Insert("🌍世界")
	got := mustApply(t, op, "héllo😀")
	if got != "héllo🌍世界" {
		t.Errorf("got %q", got)
	}
	if op.BaseLen() != 6 || op.TargetLen() != 8 {
		t.Errorf("baseLen=%d targetLen=%d, want 6 and 8", op.BaseLen(), op.TargetLen())
	}
}

func TestApplyLengthMismatch(t *testing.T) {
	op := New().Retain(3)
	if _, err := op.Apply("ab"); err == nil {
		t.Error("expected error for base length mismatch")
	}
}

func TestBuilderCanonicalization(t *testing.T) {
	// Adjacent same-type components merge.
	op := New().Retain(1).Retain(2).Insert("a").Insert("b").Delete(1).Delete(2)
	if len(op.ops) != 3 {
		t.Fatalf("expected 3 merged components, got %d: %s", len(op.ops), op)
	}
	// Insert after delete swaps into canonical insert-first order.
	a := New().Retain(1).Delete(2).Insert("x")
	b := New().Retain(1).Insert("x").Delete(2)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("delete-insert not canonicalized: %s vs %s", ja, jb)
	}
	// Insert after insert+delete merges into the existing insert.
	c := New().Insert("x").Delete(2).Insert("y")
	jc, _ := json.Marshal(c)
	if string(jc) != `["xy",-2]` {
		t.Errorf("got %s, want [\"xy\",-2]", jc)
	}
	// No-op components are dropped.
	d := New().Retain(0).Insert("").Delete(0)
	if !d.IsNoop() || len(d.ops) != 0 {
		t.Errorf("zero components should be dropped: %s", d)
	}
}

func TestJSONWireFormat(t *testing.T) {
	op := New().Retain(5).Insert(" world").Delete(2)
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[5," world",-2]` {
		t.Errorf("got %s", data)
	}
	var back Operation
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != op.String() {
		t.Errorf("roundtrip mismatch: %s vs %s", back.String(), op.String())
	}
}

func TestJSONUnmarshalRejectsInvalid(t *testing.T) {
	for _, in := range []string{`[1.5]`, `[true]`, `[[1]]`, `{"a":1}`, `"x"`} {
		var op Operation
		if err := json.Unmarshal([]byte(in), &op); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
	// Zeros are tolerated and dropped.
	var op Operation
	if err := json.Unmarshal([]byte(`[0,"a",0]`), &op); err != nil {
		t.Fatal(err)
	}
	if op.String() != `insert("a")` {
		t.Errorf("got %s", op.String())
	}
}

func TestCompose(t *testing.T) {
	a := New().Retain(5).Insert(" world") // "hello" -> "hello world"
	b := New().Retain(11).Insert("!")     // -> "hello world!"
	ab, err := Compose(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustApply(t, ab, "hello"); got != "hello world!" {
		t.Errorf("got %q", got)
	}
	if _, err := Compose(a, New().Retain(3)); err == nil {
		t.Error("expected length mismatch error")
	}
}

func TestTransform(t *testing.T) {
	// Concurrent edits on "hello": a inserts at the end, b deletes the head.
	a := New().Retain(5).Insert("!")
	b := New().Delete(1).Insert("H").Retain(4)
	a2, b2, err := Transform(a, b)
	if err != nil {
		t.Fatal(err)
	}
	viaA := mustApply(t, b2, mustApply(t, a, "hello"))
	viaB := mustApply(t, a2, mustApply(t, b, "hello"))
	if viaA != viaB || viaA != "Hello!" {
		t.Errorf("diverged: %q vs %q", viaA, viaB)
	}
	if _, _, err := Transform(a, New().Retain(3)); err == nil {
		t.Error("expected length mismatch error")
	}
}

func TestTransformInsertTieBreak(t *testing.T) {
	// Both insert at position 0; a's insert must come first on both sides.
	a := New().Insert("a").Retain(1)
	b := New().Insert("b").Retain(1)
	a2, b2, err := Transform(a, b)
	if err != nil {
		t.Fatal(err)
	}
	viaA := mustApply(t, b2, mustApply(t, a, "x"))
	viaB := mustApply(t, a2, mustApply(t, b, "x"))
	if viaA != viaB || viaA != "abx" {
		t.Errorf("got %q and %q, want \"abx\"", viaA, viaB)
	}
}

func TestTransformIndex(t *testing.T) {
	cases := []struct {
		op   *Operation
		pos  int
		want int
	}{
		{New().Insert("ab").Retain(5), 2, 4},          // insert before cursor shifts right
		{New().Retain(5).Insert("ab"), 2, 2},          // insert after cursor: unchanged
		{New().Delete(2).Retain(3), 1, 0},             // cursor inside deleted span clamps
		{New().Delete(2).Retain(3), 4, 2},             // delete before cursor shifts left
		{New().Retain(2).Insert("😀").Retain(3), 2, 3}, // insert at cursor pushes it right
		{New().Retain(5), 3, 3},                       // pure retain: unchanged
	}
	for i, c := range cases {
		if got := c.op.TransformIndex(c.pos); got != c.want {
			t.Errorf("case %d: %s TransformIndex(%d) = %d, want %d", i, c.op, c.pos, got, c.want)
		}
	}
}
