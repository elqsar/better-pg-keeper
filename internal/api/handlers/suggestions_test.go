package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestSuggestionMetadataKeepsLargeIntegers(t *testing.T) {
	// Above 2^53: a float64 round trip would print 1324954140659947800.
	const id = "1324954140659947724"
	resp := suggestionToResponse(models.Suggestion{Metadata: `{"queryid":` + id + `,"query_ids":[` + id + `]}`})
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), id) != 2 {
		t.Errorf("query ids corrupted: %s", out)
	}

	if resp := suggestionToResponse(models.Suggestion{Metadata: "not json"}); resp.Metadata != nil {
		t.Errorf("invalid metadata should be dropped, got %s", resp.Metadata)
	}
}
