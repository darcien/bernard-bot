package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bernard/discord"
)

// An admission with free slots admits immediately; the turn past capacity waits.
func TestAdmission_AdmitsUpToCapacityThenBlocks(t *testing.T) {
	a := newAdmission(2)
	for i := range 2 {
		if err := a.enter(context.Background(), time.Second); err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
	}

	if err := a.enter(context.Background(), 10*time.Millisecond); !errors.Is(err, errBusy) {
		t.Errorf("want errBusy past capacity, got %v", err)
	}
}

// Waiting past admissionWait is a refusal, not a queue: the caller gets
// errBusy so it can say so instead of answering a stale question.
func TestAdmission_ReportsBusyWhenWaitExpires(t *testing.T) {
	a := newAdmission(1)
	if err := a.enter(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := a.enter(context.Background(), 30*time.Millisecond)
	if !errors.Is(err, errBusy) {
		t.Fatalf("want errBusy, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("returned after %v, want at least the full wait", elapsed)
	}
}

// A released slot lets the next turn in.
func TestAdmission_ReleasedSlotAdmitsTheNextTurn(t *testing.T) {
	a := newAdmission(1)
	if err := a.enter(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	a.leave()

	if err := a.enter(context.Background(), 10*time.Millisecond); err != nil {
		t.Errorf("want the released slot reused, got %v", err)
	}
}

// Shutdown unblocks a waiter with the context error, not errBusy — the
// caller says "restarting", not "busy", and never hangs until the process
// dies.
func TestAdmission_AbortsOnShutdown(t *testing.T) {
	a := newAdmission(1)
	if err := a.enter(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := a.enter(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// The indicator fires as soon as the turn is admitted rather than one tick
// later: the first seconds are exactly when the channel is waiting to see
// that anything is happening.
func TestKeepTyping_FiresImmediatelyAndStops(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/typing") {
			calls.Add(1)
		}
	}))
	defer srv.Close()
	orig := discord.DiscordAPIBase
	discord.DiscordAPIBase = srv.URL
	defer func() { discord.DiscordAPIBase = orig }()

	stop := keepTyping(context.Background(), "chan-1")
	waitFor(t, func() bool { return calls.Load() >= 1 })
	stop()

	settled := calls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != settled {
		t.Errorf("indicator kept firing after stop: %d then %d", settled, got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// tryEnter must take a free slot every time: a fold that is skipped when the
// endpoint is idle would leave the session over the trigger for no reason.
func TestAdmission_TryEnterTakesAFreeSlot(t *testing.T) {
	a := newAdmission(1)
	for range 100 {
		if !a.tryEnter() {
			t.Fatal("want a free slot taken")
		}
		a.leave()
	}
	if !a.tryEnter() {
		t.Fatal("want the last slot")
	}
	if a.tryEnter() {
		t.Error("want a refusal with no slot free")
	}
}
