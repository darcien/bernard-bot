package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeConn struct {
	in   chan []byte
	sent chan []byte
}

func newFakeConn() *fakeConn {
	return &fakeConn{in: make(chan []byte, 16), sent: make(chan []byte, 16)}
}

func (f *fakeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case data := <-f.in:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeConn) Write(_ context.Context, data []byte) error {
	f.sent <- data
	return nil
}

func (f *fakeConn) Close(int, string) error { return nil }

func (f *fakeConn) expectSent(t *testing.T) payload {
	t.Helper()
	select {
	case data := <-f.sent:
		var p payload
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatalf("bad sent payload: %v", err)
		}
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a sent payload")
		return payload{}
	}
}

const helloBig = `{"op":10,"d":{"heartbeat_interval":3600000}}` // no heartbeats during test

// startRun runs the gateway against scripted fake conns and returns the
// dialed URLs channel plus a cancel/wait pair.
func startRun(t *testing.T, cfg Config, conns ...*fakeConn) (urls chan string, cancel func()) {
	t.Helper()
	old := baseBackoff
	baseBackoff = time.Millisecond
	t.Cleanup(func() { baseBackoff = old })

	queue := make(chan *fakeConn, len(conns))
	for _, c := range conns {
		queue <- c
	}
	urls = make(chan string, len(conns))
	cfg.Dial = func(_ context.Context, url string) (Conn, error) {
		select {
		case c := <-queue:
			urls <- url
			return c, nil
		default:
			return nil, errors.New("no more scripted conns")
		}
	}
	if cfg.Token == "" {
		cfg.Token = "tok"
	}

	ctx, ctxCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	t.Cleanup(func() {
		ctxCancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not stop on ctx cancel")
		}
	})
	return urls, ctxCancel
}

func TestRun_IdentifyThenResumeAfterReconnect(t *testing.T) {
	c1, c2 := newFakeConn(), newFakeConn()
	events := make(chan string, 8)
	urls, _ := startRun(t, Config{
		Intents: IntentGuilds | IntentGuildMessages,
		OnEvent: func(eventType string, _ json.RawMessage) { events <- eventType },
	}, c1, c2)

	if got := <-urls; got != defaultURL {
		t.Errorf("want first dial at default URL, got %q", got)
	}

	c1.in <- []byte(helloBig)
	identify := c1.expectSent(t)
	if identify.Op != opIdentify {
		t.Fatalf("want IDENTIFY after HELLO, got op %d", identify.Op)
	}
	var id identifyData
	_ = json.Unmarshal(identify.D, &id)
	if id.Token != "tok" || id.Intents != (IntentGuilds|IntentGuildMessages) {
		t.Errorf("want token+intents in IDENTIFY, got %+v", id)
	}

	c1.in <- []byte(`{"op":0,"t":"READY","s":1,"d":{"session_id":"s1","resume_gateway_url":"wss://resume.example"}}`)
	c1.in <- []byte(`{"op":0,"t":"MESSAGE_CREATE","s":2,"d":{"id":"m1"}}`)
	for _, want := range []string{"READY", "MESSAGE_CREATE"} {
		select {
		case got := <-events:
			if got != want {
				t.Errorf("want event %q, got %q", want, got)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}

	// Server asks for a reconnect → next conn must RESUME at the resume URL.
	c1.in <- []byte(`{"op":7}`)
	if got := <-urls; got != "wss://resume.example" {
		t.Errorf("want resume URL on second dial, got %q", got)
	}
	c2.in <- []byte(helloBig)
	resume := c2.expectSent(t)
	if resume.Op != opResume {
		t.Fatalf("want RESUME on reconnect, got op %d", resume.Op)
	}
	var rd resumeData
	_ = json.Unmarshal(resume.D, &rd)
	if rd.SessionID != "s1" || rd.Seq != 2 {
		t.Errorf("want session s1 seq 2, got %+v", rd)
	}
}

func TestRun_NonResumableInvalidSessionReidentifies(t *testing.T) {
	c1, c2 := newFakeConn(), newFakeConn()
	startRun(t, Config{}, c1, c2)

	c1.in <- []byte(helloBig)
	if p := c1.expectSent(t); p.Op != opIdentify {
		t.Fatalf("want IDENTIFY, got op %d", p.Op)
	}
	c1.in <- []byte(`{"op":0,"t":"READY","s":1,"d":{"session_id":"s1","resume_gateway_url":"wss://r"}}`)
	c1.in <- []byte(`{"op":9,"d":false}`)

	c2.in <- []byte(helloBig)
	if p := c2.expectSent(t); p.Op != opIdentify {
		t.Errorf("want fresh IDENTIFY after non-resumable invalid session, got op %d", p.Op)
	}
}

func TestRun_Heartbeats(t *testing.T) {
	c := newFakeConn()
	startRun(t, Config{}, c)

	c.in <- []byte(`{"op":10,"d":{"heartbeat_interval":50}}`)
	if p := c.expectSent(t); p.Op != opIdentify {
		t.Fatalf("want IDENTIFY first, got op %d", p.Op)
	}
	if p := c.expectSent(t); p.Op != opHeartbeat {
		t.Fatalf("want heartbeat, got op %d", p.Op)
	}
	c.in <- []byte(`{"op":11}`)
	if p := c.expectSent(t); p.Op != opHeartbeat {
		t.Errorf("want second heartbeat after ACK, got op %d", p.Op)
	}
}

func TestRun_FatalCloseCodeStops(t *testing.T) {
	old := baseBackoff
	baseBackoff = time.Millisecond
	t.Cleanup(func() { baseBackoff = old })

	dial := func(_ context.Context, _ string) (Conn, error) {
		return nil, CloseError{Code: 4004, Reason: "authentication failed"}
	}
	err := Run(context.Background(), Config{Token: "bad", Dial: dial})
	var ce CloseError
	if !errors.As(err, &ce) || ce.Code != 4004 {
		t.Errorf("want fatal 4004 error, got %v", err)
	}
}
