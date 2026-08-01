package chat

import "time"

// All chat tunables in one place; see docs/harness.md "Limits" for the
// derivation chain. Hard external ceilings these sit under: Discord message
// length (2000 UTF-16 units) and the host's process kill timeout. The
// per-LLM-call timeout (60s) lives in llm.NewClient.
const (
	// contextWindow is DeepSeek V4's token budget, the number every context
	// decision divides into. Reasonix ships the same figure and reserves
	// nothing for output, so the ratios go straight onto it.
	contextWindow = 1_000_000
	// Ratios are Reasonix's, naming the escalation points: notice, snip tool
	// results, prune then summarise, summarise regardless of fold economics.
	softRatio    = 0.5
	snipRatio    = 0.6
	compactRatio = 0.8
	forceRatio   = 0.9

	turnTimeout       = 5 * time.Minute  // whole turn; well under any ceiling
	webFetchTimeout   = 15 * time.Second // fits inside one call round
	graceMinRemaining = 90 * time.Second // one LLM call + reply headroom
	maxToolRounds     = 5                // loop cap before the grace round
	// toolResultCap is runes per tool result. Sized when the window was
	// believed to be 128k; against 1M it is a fraction of a percent and still
	// truncates link-heavy pages, dropping headlines the user can see. See
	// docs/plan-context.md.
	toolResultCap = 12000
	sessionBudget = 60000 // chars; trim trigger, sized for a couple of fetches
	sessionFloor  = 40000 // chars; trim target (hysteresis pair)
	// maxConcurrentTurns bounds turns in flight across all channels. No
	// Reasonix analogue: they run one task per session key and never cap
	// across keys. This bounds connections to the LLM endpoint, a deployment
	// concern rather than a harness one. It sits outside the timeout chain
	// above: the slot is taken before turnTimeout starts, so queueing is
	// never charged to the turn that queues.
	maxConcurrentTurns = 3
	// admissionWait bounds that queueing. Also no analogue — their scheduler
	// blocks on the caller's context with no ceiling of its own. A Discord
	// channel moves on, so a question that waited this long is better refused
	// than answered late.
	admissionWait = 60 * time.Second
	// pendingCap bounds the mentions waiting on one running turn, Reasonix's
	// queue cap. Dropping the oldest costs nothing here: the watermark never
	// passed it, so the next gap sync refetches it.
	pendingCap = 20
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
