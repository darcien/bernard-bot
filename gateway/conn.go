package gateway

import (
	"context"
	"fmt"
)

// Conn is the minimal websocket surface the gateway lifecycle needs. The
// adapter over coder/websocket implements it today; a hand-rolled RFC 6455
// client can replace that adapter later without touching lifecycle code.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close(code int, reason string) error
}

type Dialer func(ctx context.Context, url string) (Conn, error)

// CloseError surfaces the peer's websocket close code through the port so
// the lifecycle can tell fatal closes (bad token/intents) from network
// flakes worth retrying.
type CloseError struct {
	Code   int
	Reason string
}

func (e CloseError) Error() string {
	return fmt.Sprintf("websocket closed %d: %s", e.Code, e.Reason)
}

// fatalCloseCode reports Discord close codes where reconnecting can't help:
// bad auth, invalid/disallowed intents, bad shard config.
// https://discord.com/developers/docs/topics/opcodes-and-status-codes#gateway-gateway-close-event-codes
func fatalCloseCode(code int) bool {
	switch code {
	case 4004, 4010, 4011, 4012, 4013, 4014:
		return true
	}
	return false
}
