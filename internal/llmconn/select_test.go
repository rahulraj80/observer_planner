package llmconn

import "testing"

func remote(id string) Connection {
	return Connection{ID: id, Enabled: true, BaseURL: "https://api.example.com", AllowRemote: true}
}

func local(id string) Connection {
	return Connection{ID: id, Enabled: true, BaseURL: "http://127.0.0.1:11434"}
}

func TestSelect(t *testing.T) {
	t.Run("empty returns none", func(t *testing.T) {
		if _, ok := Select(nil, TaskNarrative, ""); ok {
			t.Fatal("expected no selection")
		}
	})
	t.Run("hint wins when usable", func(t *testing.T) {
		conns := []Connection{local("a"), remote("b")}
		c, ok := Select(conns, TaskNarrative, "b")
		if !ok || c.ID != "b" {
			t.Fatalf("hint not honored: %v %v", c, ok)
		}
	})
	t.Run("hint ignored when disabled or wrong task", func(t *testing.T) {
		conns := []Connection{
			{ID: "d", Enabled: false, BaseURL: "https://x"},
			{ID: "t", Enabled: true, BaseURL: "https://x", TaskRoles: []Task{TaskAsk}},
			local("l"),
		}
		c, ok := Select(conns, TaskNarrative, "d")
		if !ok || c.ID != "l" {
			t.Fatalf("disabled hint fallback: %v %v", c, ok)
		}
		c, ok = Select(conns, TaskNarrative, "t")
		if !ok || c.ID != "l" {
			t.Fatalf("wrong-task hint fallback: %v %v", c, ok)
		}
	})
	t.Run("prefers local (no-egress) over remote", func(t *testing.T) {
		conns := []Connection{remote("r"), local("l")}
		c, ok := Select(conns, TaskNarrative, "")
		if !ok || c.ID != "l" {
			t.Fatalf("local not preferred: %v %v", c, ok)
		}
	})
	t.Run("first usable fallback when no local", func(t *testing.T) {
		conns := []Connection{remote("r1"), remote("r2")}
		c, ok := Select(conns, TaskNarrative, "")
		if !ok || c.ID != "r1" {
			t.Fatalf("fallback = %v %v", c, ok)
		}
	})
	t.Run("remote without allow_remote excluded", func(t *testing.T) {
		conns := []Connection{{ID: "s", Enabled: true, BaseURL: "https://x"}}
		if _, ok := Select(conns, TaskNarrative, ""); ok {
			t.Fatal("non-allowed remote was selected")
		}
	})
	t.Run("task roles gate selection", func(t *testing.T) {
		conns := []Connection{{ID: "ask-only", Enabled: true, BaseURL: "http://127.0.0.1:9", TaskRoles: []Task{TaskAsk}}}
		if _, ok := Select(conns, TaskNarrative, ""); ok {
			t.Fatal("task-gated conn selected for other task")
		}
		if _, ok := Select(conns, TaskAsk, ""); !ok {
			t.Fatal("task-gated conn not selected for its task")
		}
	})
}

func TestConnectionHelpers(t *testing.T) {
	if !remote("x").Remote() || remote("x").Local() {
		t.Fatal("remote classification wrong")
	}
	if !local("x").Local() {
		t.Fatal("local classification wrong")
	}
	open := Connection{ID: "o", Enabled: true, BaseURL: "https://x"}
	if !open.Allows(TaskNarrative) {
		t.Fatal("empty roles must allow all tasks")
	}
	locked := Connection{ID: "l", BaseURL: "https://x", TaskRoles: []Task{TaskSummarize}}
	if locked.Allows(TaskNarrative) {
		t.Fatal("locked roles must reject other tasks")
	}
	if !locked.Allows(TaskSummarize) {
		t.Fatal("locked roles must accept their task")
	}
}
