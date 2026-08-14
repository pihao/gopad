package ot

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Compose merges two consecutive operations into one with the same effect:
// Apply(Apply(doc, a), b) == Apply(doc, Compose(a, b)).
// Requires a.TargetLen() == b.BaseLen().
func Compose(a, b *Operation) (*Operation, error) {
	if a.targetLen != b.baseLen {
		return nil, fmt.Errorf("ot: compose: first operation target length %d does not match second operation base length %d", a.targetLen, b.baseLen)
	}
	out := New()
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
		// Deletes from a and inserts from b are independent of the other
		// operation and pass straight through.
		if ok1 && op1.isDelete() {
			out.Delete(-op1.n)
			next1()
			continue
		}
		if ok2 && op2.isInsert() {
			out.Insert(op2.s)
			next2()
			continue
		}
		if !ok1 || !ok2 {
			return nil, errors.New("ot: compose: operations do not line up")
		}
		switch {
		case op1.isRetain() && op2.isRetain():
			switch {
			case op1.n > op2.n:
				out.Retain(op2.n)
				op1.n -= op2.n
				next2()
			case op1.n == op2.n:
				out.Retain(op1.n)
				next1()
				next2()
			default:
				out.Retain(op1.n)
				op2.n -= op1.n
				next1()
			}
		case op1.isInsert() && op2.isDelete():
			l, d := utf8.RuneCountInString(op1.s), -op2.n
			switch {
			case l > d:
				_, op1.s = splitRunes(op1.s, d)
				next2()
			case l == d:
				next1()
				next2()
			default:
				op2.n += l
				next1()
			}
		case op1.isInsert() && op2.isRetain():
			l := utf8.RuneCountInString(op1.s)
			switch {
			case l > op2.n:
				var head string
				head, op1.s = splitRunes(op1.s, op2.n)
				out.Insert(head)
				next2()
			case l == op2.n:
				out.Insert(op1.s)
				next1()
				next2()
			default:
				out.Insert(op1.s)
				op2.n -= l
				next1()
			}
		case op1.isRetain() && op2.isDelete():
			d := -op2.n
			switch {
			case op1.n > d:
				out.Delete(d)
				op1.n -= d
				next2()
			case op1.n == d:
				out.Delete(d)
				next1()
				next2()
			default:
				out.Delete(op1.n)
				op2.n += op1.n
				next1()
			}
		default:
			return nil, errors.New("ot: compose: operations do not line up")
		}
	}
	return out, nil
}
