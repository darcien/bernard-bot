package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"bernard/commands"
	"bernard/discord"
)

var discordAPIBase = discord.DiscordAPIBase

type registeredCommand struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: manage <register|list|delete <commandId>>")
		os.Exit(1)
	}

	appID := os.Getenv("DISCORD_APPLICATION_ID")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if appID == "" || botToken == "" {
		log.Fatal("Missing DISCORD_APPLICATION_ID or DISCORD_BOT_TOKEN")
	}

	switch os.Args[1] {
	case "register":
		doRegister(appID, botToken)
	case "list":
		doList(appID, botToken)
	case "delete":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: manage delete <commandId>")
			os.Exit(1)
		}
		doDelete(appID, botToken, os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// doRegister bulk-overwrites all commands using PUT /applications/{id}/commands.
// This replaces all existing global commands atomically.
// https://discord.com/developers/docs/interactions/application-commands#create-global-application-command
func doRegister(appID, botToken string) {
	defs := commands.CommandDefinitions()
	url := fmt.Sprintf("%s/applications/%s/commands", discordAPIBase, appID)

	body, _ := json.Marshal(defs)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		var result []registeredCommand
		if err := json.Unmarshal(respBody, &result); err != nil {
			log.Fatalf("parse response: %v", err)
		}
		fmt.Printf("Registered %d command(s):\n", len(result))
		for _, cmd := range result {
			fmt.Printf("  /%s (id=%s)\n", cmd.Name, cmd.ID)
		}
	} else {
		fmt.Printf("Failed (%d): %s\n", resp.StatusCode, string(respBody))
		os.Exit(1)
	}
}

// doList prints all globally registered commands for the application.
// https://discord.com/developers/docs/interactions/application-commands#get-global-application-commands
func doList(appID, botToken string) {
	url := fmt.Sprintf("%s/applications/%s/commands", discordAPIBase, appID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result []registeredCommand
	if err := json.Unmarshal(respBody, &result); err != nil {
		fmt.Printf("Response (%d): %s\n", resp.StatusCode, string(respBody))
		return
	}

	fmt.Printf("%d registered command(s):\n", len(result))
	for _, cmd := range result {
		fmt.Printf("  id=%-20s  /%s\n", cmd.ID, cmd.Name)
	}
}

// doDelete deletes a single command by ID.
// https://discord.com/developers/docs/interactions/application-commands#delete-global-application-command
func doDelete(appID, botToken, commandID string) {
	url := fmt.Sprintf("%s/applications/%s/commands/%s", discordAPIBase, appID, commandID)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		log.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		fmt.Printf("Deleted command id=%s\n", commandID)
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Failed (%d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}
}
