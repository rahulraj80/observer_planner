package store

import (
	"context"
	"testing"
	"time"
)

func TestAgentConfigLayerStore(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	layers := []AgentConfigLayerRow{
		{FilePath: "/h/.claude/settings.json", KeyPath: "model", Tool: "claude-code", SettingID: "claude-code/model", Layer: "l1_all_adapters", CommonKey: "model", ValueHash: "h1", ValueJSON: `"opus"`, UpdatedAt: at},
		{FilePath: "/h/.claude/settings.json", KeyPath: "env.OPENAI_API_KEY", Tool: "claude-code", Layer: "l2_adapter", ValueHash: "h2", UpdatedAt: at},
	}
	id, err := s.RecordAgentConfigSave(ctx, layers, AgentConfigWriteRow{
		At: at, Tool: "claude-code", FileID: "claude-code/user-settings", FilePath: "/h/.claude/settings.json", Layer: "l1_all_adapters",
		Origin: "dashboard", Mode: "common", ChangedKeys: []string{"model", "env.OPENAI_API_KEY"}, AfterSHA256: "abc", Created: true,
	})
	if err != nil || id == 0 {
		t.Fatalf("record: id=%d err=%v", id, err)
	}
	got, err := s.LoadAgentConfigLayers(ctx, []string{"/h/.claude/settings.json"})
	if err != nil || len(got) != 2 {
		t.Fatalf("load: %v %+v", err, got)
	}
	if got[1].KeyPath != "model" || got[1].ValueJSON != `"opus"` || got[0].ValueJSON != "" {
		t.Fatalf("rows: %+v", got)
	}
	// Upsert + delete.
	if _, err := s.RecordAgentConfigSave(ctx, []AgentConfigLayerRow{
		{FilePath: "/h/.claude/settings.json", KeyPath: "model", Tool: "claude-code", Layer: "l2_adapter", ValueHash: "h3"},
		{FilePath: "/h/.claude/settings.json", KeyPath: "env.OPENAI_API_KEY", Delete: true},
	}, AgentConfigWriteRow{Tool: "claude-code", FilePath: "/h/.claude/settings.json", AfterSHA256: "def", Origin: "cli", Mode: "form"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.LoadAgentConfigLayers(ctx, nil)
	if len(got) != 1 || got[0].Layer != "l2_adapter" || got[0].ValueHash != "h3" {
		t.Fatalf("after upsert/delete: %+v", got)
	}
	ws, err := s.ListAgentConfigWrites(ctx, "claude-code", 10)
	if err != nil || len(ws) != 2 || ws[0].Origin != "cli" || len(ws[1].ChangedKeys) != 2 || !ws[1].Created {
		t.Fatalf("writes: %v %+v", err, ws)
	}
	if _, err := s.RecordAgentConfigSave(ctx, nil, AgentConfigWriteRow{}); err == nil {
		t.Fatal("an empty write row was accepted")
	}
}
