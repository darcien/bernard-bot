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
	// tailBudget is what maintenance keeps verbatim, in tokens — Reasonix's
	// tail budget, and what the region is measured against once compactRatio
	// fires. They cap it at compactTarget (0.5) of the window; at 1M that cap
	// never binds, so it is not carried. A token budget, not a unit count, so
	// a couple of large tool results can't hold the session over the trigger
	// and re-fire the fold every turn.
	tailBudget = 16384
	// recentKeep is the fewest units maintenance will leave, Reasonix's
	// minRecentKeep: the current question and the exchange before it survive
	// even when one of them alone exceeds the budget.
	recentKeep = 2
	// Snip geometry, Reasonix's: minSnipBytes is their minPruneBytes, the
	// size below which rewriting a result saves less than the marker costs;
	// the line counts are their web_fetch SnipHint, generous head and short
	// tail because a fetched page front-loads. The byte pair is the fallback
	// for content with too few lines to split — one long line of JSON.
	minSnipBytes  = 1024
	snipHead      = 120
	snipTail      = 12
	snipHeadBytes = 12000
	snipTailBytes = 2000
	// Summarisation guards, Reasonix's: a fold smaller than minFoldTokens
	// saves less than the call costs; summaryTimeout bounds a stalled
	// summariser; a user turn under pinnedUserTokens is kept verbatim rather
	// than summarised, because a fact someone stated is not the harness's to
	// paraphrase. maxConsecutiveCompacts is their stuck latch: two folds in a
	// row that fail to clear the trigger mean the tail alone exceeds it, and
	// re-firing every turn is the loop it prevents.
	minFoldTokens          = 400
	summaryTimeout         = 90 * time.Second
	pinnedUserTokens       = 1500
	maxConsecutiveCompacts = 2

	turnTimeout       = 5 * time.Minute  // whole turn; well under any ceiling
	webFetchTimeout   = 15 * time.Second // fits inside one call round
	graceMinRemaining = 90 * time.Second // one LLM call + reply headroom
	maxToolRounds     = 5                // loop cap before the grace round
	// toolResultCap is bytes per tool result, Reasonix's maxToolOutputBytes.
	// Bytes to match unitSize, which the region is planned in. It exists to
	// stop one huge read making the *next* request too big: maintenance only
	// runs after the reply, so an oversized result would otherwise sail into
	// the following prompt unchecked. Five rounds at this cap is ~51k tokens,
	// 5% of the window.
	toolResultCap = 32 * 1024
	// citationURLCap bounds the URL in the "[N] source: <url>" line the loop
	// prefixes onto an already-capped result: without it, a long URL — the
	// model chooses it — pushes the tool message past toolResultCap by
	// however long the URL is.
	//
	// 2048 is the practical ceiling for a URL that works at all: the limit
	// legacy browsers imposed and the one most servers still default near, so
	// a longer one has very likely already failed to fetch. Past it the line
	// says the URL was omitted rather than showing a cut one — a truncated
	// URL is a broken link that looks like a working link, and the footer the
	// user reads renders the source whole from the harness's own record.
	citationURLCap = 2048
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
