package cmd

import "testing"

func TestEmbeddingsCommandUsesEmbedSpelling(t *testing.T) {
	embed, _, err := embeddingsCmd.Find([]string{"embed"})
	if err != nil {
		t.Fatalf("find embed command: %v", err)
	}
	if embed != embeddingsEmbedCmd {
		t.Fatalf("embed resolved to %q, want embeddingsEmbedCmd", embed.Use)
	}

	embedd, _, err := embeddingsCmd.Find([]string{"embedd"})
	if err == nil && embedd == embeddingsEmbedCmd {
		t.Fatalf("legacy embedd command unexpectedly resolves to embed command")
	}
}
