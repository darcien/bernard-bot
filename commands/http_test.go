package commands

import (
	"encoding/json"
	"testing"

	"bernard/discord"
)

func TestHandleHTTP(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		variant    string
		wantURL    string
	}{
		{"cat default", 200, "", "https://http.cat/200"},
		{"cat explicit", 404, "cat", "https://http.cat/404"},
		{"cat2", 404, "cat2", "https://httpcats.com/404.webp"},
		{"dog", 500, "dog", "https://http.dog/500.webp"},
		{"duck", 301, "duck", "https://httpducks.com/301.webp"},
		{"fish", 418, "fish", "https://http.fish/418.webp"},
		{"garden", 200, "garden", "https://http.garden/200.webp"},
		{"goat", 503, "goat", "https://httpgoats.com/503.webp"},
		{"mdn", 301, "mdn", "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/301"},
		{"pizza", 200, "pizza", "https://http.pizza/200.webp"},
		{"unknown variant falls back to cat", 200, "unknown", "https://http.cat/200"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var options []discord.InteractionDataOption

			sc, _ := json.Marshal(tc.statusCode)
			options = append(options, discord.InteractionDataOption{
				Name:  "status_code",
				Type:  discord.OptionTypeInteger,
				Value: sc,
			})

			if tc.variant != "" {
				v, _ := json.Marshal(tc.variant)
				options = append(options, discord.InteractionDataOption{
					Name:  "variant",
					Type:  discord.OptionTypeString,
					Value: v,
				})
			}

			ctx := CommandContext{
				InteractionData: discord.ApplicationCommandData{
					Name:    "http",
					Options: options,
				},
			}

			result, err := handleHTTP(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResponseText != tc.wantURL {
				t.Errorf("want %q, got %q", tc.wantURL, result.ResponseText)
			}
		})
	}
}
