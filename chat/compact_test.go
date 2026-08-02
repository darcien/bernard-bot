package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bernard/llm"
)

// A fact someone stated survives the fold whatever the summariser does with
// it: the digest is a paraphrase, and a paraphrase of "call me X" is not
// "call me X".
func TestSummariseRegion_KeepsSmallUserTurnsVerbatim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"## Topics\n- things"}}]}`))
	}))
	defer srv.Close()

	region := [][]llm.Message{{
		{Role: "user", Content: "darcien: remember the deploy is friday"},
		{Role: "assistant", Content: "noted"},
	}}
	got := summariseRegion(context.Background(), llm.NewClient(srv.URL, "k", "m"), region, 0.32)

	if got[0].Content != "darcien: remember the deploy is friday" {
		t.Errorf("want the user turn kept verbatim and first, got %q", got[0].Content)
	}
	last := got[len(got)-1]
	if last.Role != "user" || !strings.HasPrefix(last.Content, summaryTagOpen) {
		t.Errorf("want a tagged digest as a user turn, got %+v", last)
	}
	if !strings.Contains(last.Content, "## Topics") {
		t.Errorf("want the model's digest inside, got %q", last.Content)
	}
}

// A failed summariser must still free the context: returning the region
// unchanged would leave the session over the trigger and fold again next
// turn, paying for the same failure repeatedly.
func TestSummariseRegion_FallsBackToAMechanicalDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	region := [][]llm.Message{{
		{Role: "assistant", Content: strings.Repeat("long assistant answer ", 200)},
	}}
	got := summariseRegion(context.Background(), llm.NewClient(srv.URL, "k", "m"), region, 0.32)

	if len(got) != 1 {
		t.Fatalf("want just the digest, got %d messages", len(got))
	}
	if !strings.Contains(got[0].Content, "summary was unavailable") {
		t.Errorf("want the mechanical digest, got %q", got[0].Content)
	}
}

// An earlier digest is kept verbatim rather than folded again: re-summarising
// a summary loses whatever it already captured.
func TestPartitionFold_KeepsAnEarlierDigest(t *testing.T) {
	digest := llm.Message{Role: "user", Content: summaryTagOpen + "\nolder\n" + summaryTagClose}
	region := [][]llm.Message{{digest, {Role: "assistant", Content: "work"}}}

	kept, fold := partitionFold(region, 0.32)
	if len(kept) != 1 || kept[0].Content != digest.Content {
		t.Errorf("want the earlier digest kept, got %v", kept)
	}
	if len(fold) != 1 || fold[0].Role != "assistant" {
		t.Errorf("want only the assistant work folded, got %v", fold)
	}
}

// A pasted wall of text is not a fact to pin; it folds like any other message
// so the kept-verbatim floor cannot starve the window.
func TestPartitionFold_FoldsHugeUserTurns(t *testing.T) {
	region := [][]llm.Message{{
		{Role: "user", Content: strings.Repeat("x", pinnedUserTokens*100)},
	}}
	kept, fold := partitionFold(region, 0.32)
	if len(kept) != 0 || len(fold) != 1 {
		t.Errorf("want the oversized turn folded, kept %d folded %d", len(kept), len(fold))
	}
}
