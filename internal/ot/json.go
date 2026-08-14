package ot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// The wire format is rustpad's: a JSON array where a positive integer is a
// retain, a negative integer is a delete, and a string is an insert, e.g.
// [5, " world", -2].

// MarshalJSON implements json.Marshaler.
func (o *Operation) MarshalJSON() ([]byte, error) {
	arr := make([]any, 0, len(o.ops))
	for _, c := range o.ops {
		if c.isInsert() {
			arr = append(arr, c.s)
		} else {
			arr = append(arr, c.n)
		}
	}
	return json.Marshal(arr)
}

// UnmarshalJSON implements json.Unmarshaler. Zero-length components are
// dropped, so the result is always canonical regardless of input shape.
func (o *Operation) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var arr []any
	if err := dec.Decode(&arr); err != nil {
		return fmt.Errorf("ot: invalid operation: %w", err)
	}
	*o = Operation{}
	for _, v := range arr {
		switch t := v.(type) {
		case string:
			o.Insert(t)
		case json.Number:
			n, err := strconv.Atoi(t.String())
			if err != nil {
				return fmt.Errorf("ot: invalid operation component %q: must be an integer", t)
			}
			if n >= 0 {
				o.Retain(n)
			} else {
				o.Delete(-n)
			}
		default:
			return fmt.Errorf("ot: invalid operation component of type %T", v)
		}
	}
	return nil
}
