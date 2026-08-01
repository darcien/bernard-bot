package chat

import "time"

// All chat tunables in one place; see PLAN-CHAT-HARNESS.md "Limits" for the
// derivation chain. Hard external ceilings these sit under: Discord message
// length (2000 UTF-16 units) and the host's process kill timeout. The
// per-LLM-call timeout (60s) lives in llm.NewClient.
const (
	turnTimeout       = 5 * time.Minute  // whole turn; well under any ceiling
	webFetchTimeout   = 15 * time.Second // fits inside one call round
	graceMinRemaining = 90 * time.Second // one LLM call + reply headroom
	maxToolRounds     = 5                // loop cap before the grace round
	// toolResultCap is runes per tool result. 4000 was set blind and proved
	// far too small: a news aggregator page extracts to ~20k runes, so the
	// cap dropped 80% of it and the model truthfully reported that a
	// headline the user could see wasn't there. 12k runes is ~3k tokens —
	// affordable against a 128k context at this volume.
	toolResultCap = 12000
	sessionBudget = 60000 // chars; trim trigger, sized for a couple of fetches
	sessionFloor  = 40000 // chars; trim target (hysteresis pair)
	// maxConcurrentTurns bounds turns in flight across all channels. Each one
	// holds up to maxToolRounds × toolResultCap of transient text and one
	// connection to the LLM endpoint. It sits outside the timeout chain
	// above: the slot is taken before turnTimeout starts, so queueing is
	// never charged to the turn that queues.
	maxConcurrentTurns = 3
	// admissionWait bounds that queueing. Far under turnTimeout on
	// purpose — a question that waited longer than this has been overtaken by
	// the conversation, and a late answer is worse than an honest refusal.
	admissionWait = 60 * time.Second
	// typingInterval re-fires the typing indicator; Discord expires it at
	// ~10s and turns routinely outlive that.
	typingInterval   = 8 * time.Second
	initialSyncFetch = 20  // channel messages fetched on first sync
	gapSyncFetch     = 100 // Discord page max; gap sync between turns
	channelLineCap   = 500 // runes per synced channel message

	// ShutdownWait bounds how long shutdown waits for in-flight replies:
	// ≥ one LLM call + reply, ≤ the host's kill timeout.
	ShutdownWait = 150 * time.Second
)
