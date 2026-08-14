package ot

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Transform resolves two concurrent operations a and b against the same
// document into (a', b') such that applying a then b' yields the same
// document as applying b then a' (the TP1 property):
//
//	Apply(Apply(doc, a), b') == Apply(Apply(doc, b), a')
//
// Concurrent inserts at the same position are ordered with a's insert first.
// Requires a.BaseLen() == b.BaseLen().
func Transform(a, b *Operation) (aPrime, bPrime *Operation, err error) {
	if a.baseLen != b.baseLen {
		return nil, nil, fmt.Errorf("ot: transform: operations have different base lengths %d and %d", a.baseLen, b.baseLen)
	}
	ap, bp := New(), New()
	i, j := 0, 0
	var op1, op2 comp
	var ok1, ok2 bool
	next1 := func() {
		if i < len(a.ops) {
			op1, ok1 = a.ops[i], true
			i++
		} else {
			ok1 = false
		}
	}
	next2 := func() {
		if j < len(b.ops) {
			op2, ok2 = b.ops[j], true
			j++
		} else {
			ok2 = false
		}
	}
	next1()
	next2()
	for ok1 || ok2 {
		// Inserts happen regardless of the other side; a wins ties.
		if ok1 && op1.isInsert() {
			ap.Insert(op1.s)
			bp.Retain(utf8.RuneCountInString(op1.s))
			next1()
			continue
		}
		if ok2 && op2.isInsert() {
			ap.Retain(utf8.RuneCountInString(op2.s))
			bp.Insert(op2.s)
			next2()
			continue
		}
		if !ok1 || !ok2 {
			return nil, nil, errors.New("ot: transform: operations do not line up")
		}
		switch {
		case op1.isRetain() && op2.isRetain():
			n := min(op1.n, op2.n)
			ap.Retain(n)
			bp.Retain(n)
			op1.n -= n
			op2.n -= n
		case op1.isDelete() && op2.isDelete():
			// Both deleted the same span; nothing to do on either side.
			n := min(-op1.n, -op2.n)
			op1.n += n
			op2.n += n
		case op1.isDelete() && op2.isRetain():
			n := min(-op1.n, op2.n)
			ap.Delete(n)
			op1.n += n
			op2.n -= n
		case op1.isRetain() && op2.isDelete():
			n := min(op1.n, -op2.n)
			bp.Delete(n)
			op1.n -= n
			op2.n += n
		default:
			return nil, nil, errors.New("ot: transform: operations do not line up")
		}
		if op1.n == 0 {
			next1()
		}
		if op2.n == 0 {
			next2()
		}
	}
	return ap, bp, nil
}
