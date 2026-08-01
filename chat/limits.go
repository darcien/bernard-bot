package chat

import "time"

// All chat tunables in one place; see PLAN-CHAT-HARNESS.md "Limits" for the
// derivation chain. Hard external ceilings these sit under: Discord message
// length (2000 UTF-16 units) and the host's process kill timeout. The
// per-LLM-call timeout (60s) lives in llm.NewClient.
const (
	invocationTimeout = 5 * time.Minute  // whole turn; well under any ceiling
	webFetchTimeout   = 15 * time.Second // fits inside one call round
	graceMinRemaining = 90 * time.Second // one LLM call + reply headroom
	maxToolRounds     = 5                // loop cap before the grace round
	toolResultCap     = 4000             // runes per tool result
	sessionBudget     = 24000            // chars; trim trigger
	sessionFloor      = 16000            // chars; trim target (hysteresis pair)
	initialSyncFetch  = 20               // channel messages fetched on first sync
	gapSyncFetch      = 100              // Discord page max; gap sync between turns
	channelLineCap    = 500              // runes per synced channel message

	// ShutdownWait bounds how long shutdown waits for in-flight replies:
	// ≥ one LLM call + reply, ≤ the host's kill timeout.
	ShutdownWait = 150 * time.Second
)
