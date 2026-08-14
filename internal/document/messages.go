package document

import "gopad/internal/ot"

// Wire protocol. Every frame is a JSON object with exactly one key naming
// the message type (rustpad style), e.g. {"Edit": {"revision": 3, ...}}.

// UserInfo is a user's self-chosen display identity.
type UserInfo struct {
	Name string `json:"name"`
	Hue  int    `json:"hue"`
}

// CursorData holds one user's cursors and selections as code-point offsets.
type CursorData struct {
	Cursors    []int    `json:"cursors"`
	Selections [][2]int `json:"selections"`
}

// UserOp is one committed operation attributed to the connection that sent it.
type UserOp struct {
	ID        uint64        `json:"id"`
	Operation *ot.Operation `json:"operation"`
}

// ClientMsg is any message a client may send; exactly one field is set.
type ClientMsg struct {
	Edit        *EditMsg      `json:"Edit,omitempty"`
	SetLanguage *string       `json:"SetLanguage,omitempty"`
	ClientInfo  *UserInfo     `json:"ClientInfo,omitempty"`
	CursorData  *CursorData   `json:"CursorData,omitempty"`
	SetExpiry   *SetExpiryMsg `json:"SetExpiry,omitempty"`
}

type EditMsg struct {
	Revision  int           `json:"revision"`
	Operation *ot.Operation `json:"operation"`
}

type SetExpiryMsg struct {
	TTLSeconds int64 `json:"ttlSeconds"`
}

// ServerMsg is any message the server may send; exactly one field is set.
type ServerMsg struct {
	Identity   *uint64        `json:"Identity,omitempty"`
	History    *HistoryMsg    `json:"History,omitempty"`
	Language   *string        `json:"Language,omitempty"`
	UserInfo   *UserInfoMsg   `json:"UserInfo,omitempty"`
	UserCursor *UserCursorMsg `json:"UserCursor,omitempty"`
	Expiry     *ExpiryMsg     `json:"Expiry,omitempty"`
	Killed     *KilledMsg     `json:"Killed,omitempty"`
}

type HistoryMsg struct {
	Start      int      `json:"start"`
	Operations []UserOp `json:"operations"`
}

// UserInfoMsg announces a user joining or updating; a nil Info means the
// user left.
type UserInfoMsg struct {
	ID   uint64    `json:"id"`
	Info *UserInfo `json:"info"`
}

type UserCursorMsg struct {
	ID   uint64     `json:"id"`
	Data CursorData `json:"data"`
}

type ExpiryMsg struct {
	TTLSeconds int64 `json:"ttlSeconds"`
	ExpiresAt  int64 `json:"expiresAt"` // unix seconds
}

type KilledMsg struct {
	Reason string `json:"reason"`
}
