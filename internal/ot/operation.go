// Package ot implements the operational-transformation model used by
// rustpad (a port of the operational-transform / ot.js design).
//
// All lengths and offsets count Unicode code points — never bytes and
// never UTF-16 code units. The frontend is responsible for converting
// editor offsets to code-point offsets before talking to the server.
package ot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// comp is a single component of an operation. Exactly one meaning applies:
// s != "" means Insert(s); otherwise n > 0 means Retain(n) and n < 0 means
// Delete(-n). The zero value is invalid and never stored.
type comp struct {
	n int
	s string
}

func (c comp) isRetain() bool { return c.s == "" && c.n > 0 }
func (c comp) isDelete() bool { return c.s == "" && c.n < 0 }
func (c comp) isInsert() bool { return c.s != "" }

// Operation is an ordered sequence of Retain/Insert/Delete components
// describing an edit against a document of exactly BaseLen code points.
// Build one with New followed by chained Retain/Insert/Delete calls.
type Operation struct {
	ops       []comp
	baseLen   int
	targetLen int
}

func New() *Operation { return &Operation{} }

// BaseLen is the length (in code points) of the document this operation
// applies to.
func (o *Operation) BaseLen() int { return o.baseLen }

// TargetLen is the length (in code points) of the document after applying
// this operation.
func (o *Operation) TargetLen() int { return o.targetLen }

// IsNoop reports whether applying the operation changes nothing.
func (o *Operation) IsNoop() bool {
	return len(o.ops) == 0 || (len(o.ops) == 1 && o.ops[0].isRetain())
}

// Retain skips over n code points of the document. n <= 0 is a no-op.
func (o *Operation) Retain(n int) *Operation {
	if n <= 0 {
		return o
	}
	o.baseLen += n
	o.targetLen += n
	if l := len(o.ops); l > 0 && o.ops[l-1].isRetain() {
		o.ops[l-1].n += n
	} else {
		o.ops = append(o.ops, comp{n: n})
	}
	return o
}

// Delete removes the next n code points of the document. n <= 0 is a no-op.
func (o *Operation) Delete(n int) *Operation {
	if n <= 0 {
		return o
	}
	o.baseLen += n
	if l := len(o.ops); l > 0 && o.ops[l-1].isDelete() {
		o.ops[l-1].n -= n
	} else {
		o.ops = append(o.ops, comp{n: -n})
	}
	return o
}

// Insert adds s at the current position. An empty s is a no-op.
//
// When an insert directly follows a delete the insert is stored first:
// delete-then-insert and insert-then-delete are equivalent, and enforcing
// one canonical order keeps equal operations structurally equal.
func (o *Operation) Insert(s string) *Operation {
	if s == "" {
		return o
	}
	o.targetLen += utf8.RuneCountInString(s)
	l := len(o.ops)
	switch {
	case l > 0 && o.ops[l-1].isInsert():
		o.ops[l-1].s += s
	case l > 0 && o.ops[l-1].isDelete():
		if l > 1 && o.ops[l-2].isInsert() {
			o.ops[l-2].s += s
		} else {
			o.ops = append(o.ops, o.ops[l-1])
			o.ops[l-1] = comp{s: s}
		}
	default:
		o.ops = append(o.ops, comp{s: s})
	}
	return o
}

// Apply applies the operation to doc and returns the resulting text.
// The document must be exactly BaseLen code points long.
func (o *Operation) Apply(doc string) (string, error) {
	runes := []rune(doc)
	if len(runes) != o.baseLen {
		return "", fmt.Errorf("ot: apply: operation base length %d does not match document length %d", o.baseLen, len(runes))
	}
	var b strings.Builder
	pos := 0
	for _, c := range o.ops {
		switch {
		case c.isRetain():
			b.WriteString(string(runes[pos : pos+c.n]))
			pos += c.n
		case c.isInsert():
			b.WriteString(c.s)
		default: // delete
			pos += -c.n
		}
	}
	return b.String(), nil
}

// TransformIndex maps a cursor position (code-point offset) in the document
// before the operation to the corresponding position after it.
func (o *Operation) TransformIndex(pos int) int {
	index := pos
	newIndex := pos
	for _, c := range o.ops {
		switch {
		case c.isRetain():
			index -= c.n
		case c.isInsert():
			newIndex += utf8.RuneCountInString(c.s)
		default: // delete
			n := -c.n
			newIndex -= min(index, n)
			index -= n
		}
		if index < 0 {
			break
		}
	}
	return newIndex
}

// String renders the operation in a compact debug form, e.g.
// `retain(3) insert("ab") delete(2)`.
func (o *Operation) String() string {
	parts := make([]string, 0, len(o.ops))
	for _, c := range o.ops {
		switch {
		case c.isRetain():
			parts = append(parts, fmt.Sprintf("retain(%d)", c.n))
		case c.isInsert():
			parts = append(parts, fmt.Sprintf("insert(%q)", c.s))
		default:
			parts = append(parts, fmt.Sprintf("delete(%d)", -c.n))
		}
	}
	return strings.Join(parts, " ")
}

// splitRunes splits s after n code points.
func splitRunes(s string, n int) (head, tail string) {
	r := []rune(s)
	return string(r[:n]), string(r[n:])
}
