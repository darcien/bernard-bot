package gateway

import (
	"context"
	"sync"

	"github.com/coder/websocket"
)

// DialWebsocket adapts github.com/coder/websocket to the Conn port — the
// only file in the codebase that imports the dependency.
func DialWebsocket(ctx context.Context, url string) (Conn, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(4 << 20) // READY payloads can be large
	return &wsConn{c: c}, nil
}

type wsConn struct {
	c  *websocket.Conn
	mu sync.Mutex // the lib forbids concurrent writes
}

func (w *wsConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := w.c.Read(ctx)
	if err != nil {
		if code := websocket.CloseStatus(err); code != -1 {
			return nil, CloseError{Code: int(code), Reason: err.Error()}
		}
		return nil, err
	}
	return data, nil
}

func (w *wsConn) Write(ctx context.Context, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.Write(ctx, websocket.MessageText, data)
}

func (w *wsConn) Close(code int, reason string) error {
	return w.c.Close(websocket.StatusCode(code), reason)
}
