// Package ids generates URL-safe random identifiers.
package ids

import "crypto/rand"

// The 64-character alphabet makes every byte map uniformly to one symbol
// (6 bits each), so a 16-character id carries 96 bits of entropy. All
// characters match the server's document id pattern [A-Za-z0-9_-].
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

// New returns a random id of n characters.
func New(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return string(b)
}
