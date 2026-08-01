package chat

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"bernard/discord"
)

// errBusy means the admission wait expired before a slot opened.
var errBusy = errors.New("chat: no turn slot available")

// admission decides whether a turn may run, across every channel. The
// resource it protects is the LLM endpoint, and a turn — not a call — is the
// unit that holds a slot: admitting per call would let channels interleave
// their rounds, so every reply gets slower and a turn can starve after
// already paying for four rounds, which is the worst place to wait.
//
// The tool loop is sequential, so bounding turns bounds concurrent calls to
// the endpoint at the same number.
//
// The buffered channel is the usual Go counting-semaphore idiom, kept behind
// enter/leave so callers speak about turns rather than about slots.
type admission struct {
	slots chan struct{}
}

func newAdmission(n int) *admission {
	return &admission{slots: make(chan struct{}, n)}
}

// enter admits one turn, waiting at most wait for room. It returns errBusy
// when the wait expires — past that the channel has moved on, and an honest
// refusal beats a stale answer — or the context error when the service is
// shutting down. That last case matters: Cancel only cancels the service
// context, and a turn's own context does not exist yet, so without it a
// waiter would hang silently until the process died.
func (a *admission) enter(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a.slots <- struct{}{}:
		return nil
	case <-timer.C:
		return errBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// leave releases the turn's slot. Only call it after enter returned nil.
func (a *admission) leave() { <-a.slots }

// keepTyping shows "Bernard is typing..." until the returned stop is called.
// stop ends the ticker, not the request in flight: a TriggerTyping already
// under way runs to its own timeout. Harmless — the indicator expires on its
// own — but it means stop is not preemptive.
// Discord expires the indicator after ~10s while a turn can run for a minute,
// so it re-fires on a ticker. It starts before admission, not after: a
// mention waiting for a slot has not begun any work, and one indicator that
// lapses after ten seconds looks exactly like a dead bot.
func keepTyping(ctx context.Context, channelID string) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(typingInterval)
		defer ticker.Stop()
		for {
			if err := discord.TriggerTyping(channelID); err != nil {
				slog.Debug("typing indicator failed", "channel", channelID, "err", err)
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	return cancel
}
