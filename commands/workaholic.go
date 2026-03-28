package commands

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"bernard/discord"
)

const (
	workaholicPrefix    = "🐴"
	workaholicSeparator = " \u2043 " // " ⁃ " https://www.ascii-code.com/character/%E2%90%9F

	workaholicTypeOT = "OT"
	workaholicTypePH = "PH"

	noWorkaholicMessage = "No workaholic detected yet, keep working!"
)

var (
	rgxDateAndMonth = regexp.MustCompile(`[0-3]?\d \w{3}`)
	rgxOT           = regexp.MustCompile(`(?i)(?:ot|overtime)`)
	rgxDuration     = regexp.MustCompile(`(?i)\d?\d\s?(?:jam|hours?)`)
)

// workaholicAPIClient holds injectable API functions for testability.
type workaholicAPIClient struct {
	getMessages func(channelID string, limit int) ([]discord.Message, error)
	getMembers  func(guildID string, limit int) ([]discord.Member, error)
}

// handleWorkaholic is the production handler, using the real Discord API.
var handleWorkaholic = makeWorkaholicHandler(workaholicAPIClient{
	getMessages: discord.GetMessagesFromChannel,
	getMembers:  discord.GetGuildMembers,
})

// makeWorkaholicHandler returns a Handler using the provided API client.
// Tests pass a mock client; production uses the default above.
func makeWorkaholicHandler(api workaholicAPIClient) Handler {
	return func(ctx CommandContext) (CommandResult, error) {
		for _, opt := range ctx.InteractionData.Options {
			if opt.Type == discord.OptionTypeSubCommand {
				switch opt.Name {
				case "add":
					return handleWorkaholicAdd(ctx, opt.Options)
				case "check":
					return handleWorkaholicCheck(ctx, opt.Options, api)
				}
			}
		}
		return CommandResult{
			ResponseText: fmt.Sprintf("💣 Unhandled subcommand!\nraw data=%v", ctx.InteractionData),
		}, nil
	}
}

// --- add subcommand ---

func handleWorkaholicAdd(ctx CommandContext, opts []discord.InteractionDataOption) (CommandResult, error) {
	var what, when string
	duration := 0
	wType := workaholicTypeOT // default

	for _, opt := range opts {
		switch opt.Name {
		case "what":
			what = opt.StringValue()
		case "when":
			when = opt.StringValue()
		case "duration":
			duration = opt.IntValue()
		case "type":
			v := opt.StringValue()
			if v == workaholicTypeOT || v == workaholicTypePH {
				wType = v
			}
		}
	}

	return CommandResult{ResponseText: formatWorkaholicAdd(ctx.User.ID, what, when, duration, wType)}, nil
}

// formatWorkaholicAdd formats a workaholic entry message.
// Matches formatWorkaholicAddCommand in workaholic.ts.
func formatWorkaholicAdd(userID, what, when string, duration int, wType string) string {
	parts := []string{
		workaholicPrefix,
		"<@" + userID + ">",
		wType,
		when,
		fmt.Sprintf("%dh", duration),
		what,
	}
	return strings.Join(parts, workaholicSeparator)
}

// --- check subcommand ---

type workaholicEntry struct {
	userID   string
	what     string
	when     string
	duration string
	wType    string
}

// parseMessage extracts a workaholic entry from a message content string.
// Returns nil if the content is not a valid workaholic entry.
// Matches parseMessageForSummary in workaholic.ts.
func parseMessage(content string) *workaholicEntry {
	parts := strings.Split(content, workaholicSeparator)
	// Expected: [prefix, userMention, type, when, hDuration, ...what]
	if len(parts) < 6 {
		return nil
	}

	userMention := parts[1]
	wType := parts[2]
	when := parts[3]
	hDuration := parts[4]
	what := strings.Join(parts[5:], workaholicSeparator)

	if strings.TrimSpace(what) == "" {
		return nil
	}

	// userMention = "<@snowflakeId>"
	if len(userMention) < 4 {
		return nil
	}
	userID := userMention[2 : len(userMention)-1]

	// hDuration = "7h" → duration = "7"
	if len(hDuration) < 1 {
		return nil
	}
	duration := hDuration[:len(hDuration)-1]

	return &workaholicEntry{
		userID:   userID,
		what:     what,
		when:     when,
		duration: duration,
		wType:    wType,
	}
}

// isMessagePartialMatch returns true if the message looks like an unformatted workaholic entry.
// Matches isMessagePartialMatch in workaholic.ts.
func isMessagePartialMatch(content string) bool {
	if strings.Contains(content, "```") {
		return false
	}
	containsOT := rgxOT.MatchString(content)
	if rgxDuration.MatchString(content) {
		return containsOT
	}
	return containsOT && rgxDateAndMonth.MatchString(content)
}

func handleWorkaholicCheck(ctx CommandContext, opts []discord.InteractionDataOption, api workaholicAPIClient) (CommandResult, error) {
	doMonthMatching := true
	var who string

	for _, opt := range opts {
		switch opt.Name {
		case "when":
			if strings.Contains(opt.StringValue(), "no-month-matching") {
				doMonthMatching = false
			}
		case "who":
			if opt.Type == discord.OptionTypeUser {
				who = opt.StringValue()
			}
		}
	}

	// Fetch messages and guild members concurrently — matches Promise.all in workaholic.ts.
	type msgResult struct {
		messages []discord.Message
		err      error
	}
	type memResult struct {
		members []discord.Member
		err     error
	}

	msgCh := make(chan msgResult, 1)
	memCh := make(chan memResult, 1)

	go func() {
		msgs, err := api.getMessages(ctx.ChannelID, 100)
		msgCh <- msgResult{msgs, err}
	}()
	go func() {
		mems, err := api.getMembers(ctx.GuildID, 50)
		memCh <- memResult{mems, err}
	}()

	mr := <-msgCh
	memr := <-memCh
	if mr.err != nil {
		return CommandResult{}, mr.err
	}
	if memr.err != nil {
		return CommandResult{}, memr.err
	}

	// Filter by current month (UTC) — matches GMT+0 behavior in workaholic.ts.
	thisMonth := time.Now().UTC().Format("2006-01")
	var thisMonthMessages []discord.Message
	for _, m := range mr.messages {
		if !doMonthMatching || (len(m.Timestamp) >= 7 && m.Timestamp[:7] == thisMonth) {
			thisMonthMessages = append(thisMonthMessages, m)
		}
	}

	if len(thisMonthMessages) == 0 {
		return CommandResult{ResponseText: noWorkaholicMessage}, nil
	}

	// Filter by mentioned user if specified.
	var matchingUserMessages []discord.Message
	for _, m := range thisMonthMessages {
		if who == "" {
			matchingUserMessages = append(matchingUserMessages, m)
		} else {
			for _, u := range m.Mentions {
				if u.ID == who {
					matchingUserMessages = append(matchingUserMessages, m)
					break
				}
			}
		}
	}

	enablePartialMatching := who == ""
	prefixWithSep := workaholicPrefix + workaholicSeparator

	var partialMatchMessages []discord.Message
	var matchingEntries []workaholicEntry

	// Iterate in reverse (oldest first) — Discord returns messages newest first.
	for i := len(matchingUserMessages) - 1; i >= 0; i-- {
		msg := matchingUserMessages[i]
		if strings.HasPrefix(msg.Content, prefixWithSep) {
			if entry := parseMessage(msg.Content); entry != nil {
				matchingEntries = append(matchingEntries, *entry)
			} else if enablePartialMatching {
				partialMatchMessages = append(partialMatchMessages, msg)
			}
		} else if enablePartialMatching && msg.WebhookID == "" && !msg.Author.Bot && isMessagePartialMatch(msg.Content) {
			partialMatchMessages = append(partialMatchMessages, msg)
		}
	}

	nicknames := buildNicknameMap(memr.members)

	summaryTable := ""
	if len(matchingEntries) > 0 {
		summaryTable = makeSummaryTable(matchingEntries, nicknames)
	}

	partialWarning := ""
	if enablePartialMatching {
		partialWarning = makePartialMatchWarning(partialMatchMessages, nicknames)
	}

	if summaryTable == "" {
		if partialWarning != "" {
			return CommandResult{ResponseText: "No exact matches found but...\n" + partialWarning}, nil
		}
		return CommandResult{ResponseText: noWorkaholicMessage}, nil
	}

	parts := []string{summaryTable}
	if partialWarning != "" {
		parts = append(parts, partialWarning)
	}
	return CommandResult{ResponseText: strings.Join(parts, "\n")}, nil
}

// --- helpers ---

func buildNicknameMap(members []discord.Member) map[string]string {
	m := make(map[string]string, len(members))
	for _, member := range members {
		if member.User != nil {
			nick := member.Nick
			if nick == "" {
				nick = member.User.Username
			}
			m[member.User.ID] = nick
		}
	}
	return m
}

// makeSummaryTable renders workaholic entries as a fenced markdown table.
// Matches makeSummaryTable in workaholic.ts.
func makeSummaryTable(entries []workaholicEntry, nicknames map[string]string) string {
	header := []string{"Who", "When", "How Long", "What", "Type"}
	rows := [][]string{header}

	for _, e := range entries {
		who := nicknames[e.userID]
		if who == "" {
			who = "<@" + e.userID + ">"
		}
		rows = append(rows, []string{who, e.when, e.duration, e.what, e.wType})
	}

	return "```markdown\n" + markdownTable(rows) + "\n```"
}

// makePartialMatchWarning builds a warning message listing messages that look like
// unformatted workaholic entries. Returns empty string when there are no matches.
// Matches makePartialMatchWarning in workaholic.ts.
func makePartialMatchWarning(messages []discord.Message, nicknames map[string]string) string {
	if len(messages) == 0 {
		return ""
	}
	lines := make([]string, 0, len(messages)+1)
	lines = append(lines, fmt.Sprintf("I also found %d message(s) that might worth some workaholic points:", len(messages)))
	for _, m := range messages {
		who := nicknames[m.Author.ID]
		if who == "" {
			who = "<@" + m.Author.ID + ">"
		}
		lines = append(lines, fmt.Sprintf("- %s, %s", who, m.Content))
	}
	return strings.Join(lines, "\n")
}

// markdownTable renders rows as a GFM table with left-aligned, padded columns.
// Matches the output of the markdown-table npm package used in workaholic.ts.
func markdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	cols := len(rows[0])
	widths := make([]int, cols)
	for _, row := range rows {
		for j, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[j] {
				widths[j] = n
			}
		}
	}

	lines := make([]string, 0, len(rows)+1)
	for i, row := range rows {
		var sb strings.Builder
		sb.WriteString("|")
		for j, cell := range row {
			sb.WriteString(" ")
			sb.WriteString(cell)
			sb.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(cell)))
			sb.WriteString(" |")
		}
		lines = append(lines, sb.String())

		if i == 0 {
			var sb strings.Builder
			sb.WriteString("|")
			for _, w := range widths {
				sb.WriteString(" ")
				sb.WriteString(strings.Repeat("-", w))
				sb.WriteString(" |")
			}
			lines = append(lines, sb.String())
		}
	}

	return strings.Join(lines, "\n")
}
