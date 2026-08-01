// Package gateway is a minimal Discord Gateway (websocket) client: connect,
// identify, heartbeat, dispatch events, resume on drops. It knows nothing
// about what the events mean — consumers get the raw payloads via OnEvent.
// https://discord.com/developers/docs/topics/gateway
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"time"
)

const defaultURL = "wss://gateway.discord.gg/?v=10&encoding=json"

const (
	IntentGuilds        = 1 << 0
	IntentGuildMessages = 1 << 9
)

const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
)

// baseBackoff is a var so tests can shrink reconnect waits.
var baseBackoff = time.Second

type Config struct {
	Token   string
	Intents int
	// OnEvent receives every dispatch event's type and raw payload,
	// called from the read loop — do heavy work in a goroutine.
	OnEvent func(eventType string, data json.RawMessage)
	// Dial defaults to DialWebsocket (the coder/websocket adapter).
	Dial Dialer
	// URL defaults to the public gateway endpoint.
	URL string
}

type session struct {
	id        string
	resumeURL string
	seq       atomic.Int64
	ready     bool // READY/RESUMED seen this connection; resets backoff
}

type payload struct {
	Op int             `json:"op"`
	T  string          `json:"t,omitempty"`
	S  int64           `json:"s,omitempty"`
	D  json.RawMessage `json:"d,omitempty"`
}

// Run connects and serves gateway events until ctx is done, reconnecting
// with exponential backoff (reset after every healthy session). Returns nil
// on ctx cancellation; an error only for fatal close codes (bad token or
// intents) where retrying cannot help.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Dial == nil {
		cfg.Dial = DialWebsocket
	}
	if cfg.URL == "" {
		cfg.URL = defaultURL
	}

	sess := &session{}
	backoff := baseBackoff
	for {
		sess.ready = false
		err := runOnce(ctx, cfg, sess)
		if ctx.Err() != nil {
			return nil
		}
		var ce CloseError
		if errors.As(err, &ce) && fatalCloseCode(ce.Code) {
			return fmt.Errorf("gateway: fatal close: %w", ce)
		}
		if sess.ready {
			backoff = baseBackoff
		}
		slog.Warn("gateway disconnected, reconnecting", "err", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		backoff = min(backoff*2, time.Minute)
	}
}

// runOnce serves a single connection until it drops or ctx is done.
func runOnce(ctx context.Context, cfg Config, sess *session) error {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	url := cfg.URL
	if sess.id != "" && sess.resumeURL != "" {
		url = sess.resumeURL
	}
	conn, err := cfg.Dial(connCtx, url)
	if err != nil {
		return err
	}
	defer conn.Close(1000, "")

	interval, err := readHello(connCtx, conn)
	if err != nil {
		return err
	}

	if sess.id != "" {
		err = send(connCtx, conn, payload{Op: opResume, D: mustJSON(resumeData{
			Token: cfg.Token, SessionID: sess.id, Seq: sess.seq.Load(),
		})})
	} else {
		err = send(connCtx, conn, payload{Op: opIdentify, D: mustJSON(identifyData{
			Token: cfg.Token, Intents: cfg.Intents,
			Properties: identifyProperties{OS: runtime.GOOS, Browser: "bernard-bot", Device: "bernard-bot"},
		})})
	}
	if err != nil {
		return err
	}

	var acked atomic.Bool
	acked.Store(true)
	go heartbeater(connCtx, conn, interval, sess, &acked, cancel)

	for {
		data, err := conn.Read(connCtx)
		if err != nil {
			return err
		}
		var p payload
		if err := json.Unmarshal(data, &p); err != nil {
			slog.Warn("gateway: bad payload", "err", err)
			continue
		}
		switch p.Op {
		case opDispatch:
			sess.seq.Store(p.S)
			switch p.T {
			case "READY":
				var d struct {
					SessionID string `json:"session_id"`
					ResumeURL string `json:"resume_gateway_url"`
				}
				_ = json.Unmarshal(p.D, &d)
				sess.id, sess.resumeURL, sess.ready = d.SessionID, d.ResumeURL, true
				slog.Info("gateway ready")
			case "RESUMED":
				sess.ready = true
				slog.Info("gateway resumed")
			}
			if cfg.OnEvent != nil {
				cfg.OnEvent(p.T, p.D)
			}
		case opHeartbeat: // server asked for an immediate beat
			if err := send(connCtx, conn, heartbeatPayload(sess.seq.Load())); err != nil {
				return err // same as a failed regular beat: reconnect now
			}
		case opReconnect:
			return errors.New("gateway: server requested reconnect")
		case opInvalidSession:
			var resumable bool
			_ = json.Unmarshal(p.D, &resumable)
			if !resumable {
				sess.id, sess.resumeURL = "", ""
			}
			return fmt.Errorf("gateway: invalid session (resumable=%v)", resumable)
		case opHeartbeatACK:
			acked.Store(true)
		}
	}
}

func readHello(ctx context.Context, conn Conn) (time.Duration, error) {
	data, err := conn.Read(ctx)
	if err != nil {
		return 0, err
	}
	var hello payload
	if err := json.Unmarshal(data, &hello); err != nil {
		return 0, err
	}
	if hello.Op != opHello {
		return 0, fmt.Errorf("gateway: want HELLO, got op %d", hello.Op)
	}
	var d struct {
		HeartbeatInterval int `json:"heartbeat_interval"` // ms
	}
	if err := json.Unmarshal(hello.D, &d); err != nil {
		return 0, err
	}
	return time.Duration(d.HeartbeatInterval) * time.Millisecond, nil
}

// heartbeater beats every interval (first beat at a random fraction of it,
// per the docs). A beat without an ACK since the previous one means a zombie
// connection: cancel so the read loop unblocks and Run reconnects.
func heartbeater(ctx context.Context, conn Conn, interval time.Duration, sess *session, acked *atomic.Bool, cancel context.CancelFunc) {
	t := time.NewTimer(time.Duration(rand.Float64() * float64(interval)))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !acked.Swap(false) {
			slog.Warn("gateway: missed heartbeat ACK, dropping zombie connection")
			cancel()
			return
		}
		if err := send(ctx, conn, heartbeatPayload(sess.seq.Load())); err != nil {
			cancel()
			return
		}
		t.Reset(interval)
	}
}

func heartbeatPayload(seq int64) payload {
	if seq == 0 {
		return payload{Op: opHeartbeat, D: json.RawMessage("null")}
	}
	return payload{Op: opHeartbeat, D: mustJSON(seq)}
}

func send(ctx context.Context, conn Conn, p payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return conn.Write(ctx, data)
}

type identifyData struct {
	Token      string             `json:"token"`
	Intents    int                `json:"intents"`
	Properties identifyProperties `json:"properties"`
}

type identifyProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

type resumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
