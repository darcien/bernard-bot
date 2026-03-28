package commands

import "fmt"

type httpVariant struct {
	label    string
	buildURL func(statusCode int) string
}

// Sorted alphabetically by label — matches sortBy(v => v.label) in http.ts.
var httpVariants = []httpVariant{
	{"cat", func(c int) string { return fmt.Sprintf("https://http.cat/%d", c) }},
	{"cat2", func(c int) string { return fmt.Sprintf("https://httpcats.com/%d.webp", c) }},
	{"dog", func(c int) string { return fmt.Sprintf("https://http.dog/%d.webp", c) }},
	{"duck", func(c int) string { return fmt.Sprintf("https://httpducks.com/%d.webp", c) }},
	{"fish", func(c int) string { return fmt.Sprintf("https://http.fish/%d.webp", c) }},
	{"garden", func(c int) string { return fmt.Sprintf("https://http.garden/%d.webp", c) }},
	{"goat", func(c int) string { return fmt.Sprintf("https://httpgoats.com/%d.webp", c) }},
	{"mdn", func(c int) string {
		return fmt.Sprintf("https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/%d", c)
	}},
	{"pizza", func(c int) string { return fmt.Sprintf("https://http.pizza/%d.webp", c) }},
}

var defaultHTTPVariant = httpVariants[0] // "cat"

func handleHTTP(ctx CommandContext) (CommandResult, error) {
	statusCode := 404
	variant := defaultHTTPVariant

	for _, opt := range ctx.InteractionData.Options {
		switch opt.Name {
		case "status_code":
			statusCode = opt.IntValue()
		case "variant":
			v := opt.StringValue()
			for _, hv := range httpVariants {
				if hv.label == v {
					variant = hv
					break
				}
			}
		}
	}

	return CommandResult{ResponseText: variant.buildURL(statusCode)}, nil
}
