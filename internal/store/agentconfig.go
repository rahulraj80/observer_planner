package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The adapter config editor's node-local layer store (agent migration 155).
//
// THIS FILE IS THE ONE OWNER of agent_config_layers and agent_config_writes
// (CLAUDE.md module-boundary rule #4). The editor (internal/agentconfigsvc)
// records each save here AFTER the file write and its read-back succeeded:
// the authored layer of every key it wrote and one history row naming the
// keys (never their values).
//
// NODE-LOCAL: neither table is named in internal/store/orgpush.go (pinned
// in the forbidden-table sentinel, tests/invariant/privacy_test.go).

// AgentConfigLayerRow is one authored key.
type AgentConfigLayerRow struct {
	FilePath    string
	KeyPath     string
	Tool        string
	SettingID   string
	Layer       string
	CommonKey   string
	ProjectRoot string
	ValueHash   string
	// ValueJSON is the authored value as JSON; empty for a secret.
	ValueJSON string
	// Delete removes the row instead (the key was deleted from the file).
	Delete    bool
	UpdatedAt time.Time
}

// AgentConfigWriteRow is one history row.
type AgentConfigWriteRow struct {
	ID           int64
	At           time.Time
	Tool         string
	FileID       string
	FilePath     string
	Layer        string
	CommonKey    string
	Origin       string
	Mode         string
	ChangedKeys  []string
	BeforeSHA256 string
	AfterSHA256  string
	BackupPath   string
	Created      bool
	Warnings     int
}

// RecordAgentConfigSave writes the layer rows and the history row of one
// save in one transaction.
func (s *Store) RecordAgentConfigSave(ctx context.Context, layers []AgentConfigLayerRow, write AgentConfigWriteRow) (int64, error) {
	if strings.TrimSpace(write.Tool) == "" || strings.TrimSpace(write.FilePath) == "" || write.AfterSHA256 == "" {
		return 0, errors.New("store.RecordAgentConfigSave: tool, file path and after sha256 are required")
	}
	keys, err := json.Marshal(agentConfigKeys(write.ChangedKeys))
	if err != nil {
		return 0, fmt.Errorf("store.RecordAgentConfigSave: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store.RecordAgentConfigSave: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, l := range layers {
		at := l.UpdatedAt
		if at.IsZero() {
			at = time.Now()
		}
		if l.Delete {
			if _, err := tx.ExecContext(ctx, `DELETE FROM agent_config_layers WHERE file_path = ? AND key_path = ?`, l.FilePath, l.KeyPath); err != nil {
				return 0, fmt.Errorf("store.RecordAgentConfigSave: delete layer: %w", err)
			}
			continue
		}
		var val any
		if l.ValueJSON != "" {
			val = l.ValueJSON
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_config_layers
			(file_path, key_path, tool, setting_id, layer, common_key, project_root, value_hash, value_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(file_path, key_path) DO UPDATE SET
				tool = excluded.tool, setting_id = excluded.setting_id, layer = excluded.layer,
				common_key = excluded.common_key, project_root = excluded.project_root,
				value_hash = excluded.value_hash, value_json = excluded.value_json, updated_at = excluded.updated_at`,
			l.FilePath, l.KeyPath, l.Tool, l.SettingID, l.Layer, l.CommonKey, l.ProjectRoot, l.ValueHash, val,
			at.UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, fmt.Errorf("store.RecordAgentConfigSave: upsert layer: %w", err)
		}
	}
	at := write.At
	if at.IsZero() {
		at = time.Now()
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO agent_config_writes
		(at, tool, file_id, file_path, layer, common_key, origin, mode, changed_keys, before_sha256, after_sha256, backup_path, created, warnings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		at.UTC().Format(time.RFC3339Nano), write.Tool, write.FileID, write.FilePath, write.Layer, write.CommonKey,
		write.Origin, write.Mode, string(keys), write.BeforeSHA256, write.AfterSHA256, write.BackupPath,
		boolInt(write.Created), write.Warnings)
	if err != nil {
		return 0, fmt.Errorf("store.RecordAgentConfigSave: insert write: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store.RecordAgentConfigSave: commit: %w", err)
	}
	return id, nil
}

// LoadAgentConfigLayers returns the authored rows of the given files (all
// rows when filePaths is empty).
func (s *Store) LoadAgentConfigLayers(ctx context.Context, filePaths []string) ([]AgentConfigLayerRow, error) {
	q := `SELECT file_path, key_path, tool, setting_id, layer, common_key, project_root, value_hash, COALESCE(value_json, ''), updated_at
		FROM agent_config_layers`
	var args []any
	if len(filePaths) > 0 {
		q += ` WHERE file_path IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filePaths)), ",") + `)` //nolint:gosec // G202: the concatenated fragments are fixed SQL with ? placeholders; values bind as args
		for _, p := range filePaths {
			args = append(args, p)
		}
	}
	q += ` ORDER BY file_path, key_path`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.LoadAgentConfigLayers: %w", err)
	}
	defer rows.Close()
	var out []AgentConfigLayerRow
	for rows.Next() {
		var r AgentConfigLayerRow
		var at string
		if err := rows.Scan(&r.FilePath, &r.KeyPath, &r.Tool, &r.SettingID, &r.Layer, &r.CommonKey, &r.ProjectRoot, &r.ValueHash, &r.ValueJSON, &at); err != nil {
			return nil, fmt.Errorf("store.LoadAgentConfigLayers: scan: %w", err)
		}
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAgentConfigWrites returns the newest history rows first, optionally
// for one tool.
func (s *Store) ListAgentConfigWrites(ctx context.Context, tool string, limit int) ([]AgentConfigWriteRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT id, at, tool, file_id, file_path, layer, common_key, origin, mode, changed_keys,
		before_sha256, after_sha256, backup_path, created, warnings FROM agent_config_writes`
	var args []any
	if tool != "" {
		q += ` WHERE tool = ?`
		args = append(args, tool)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.ListAgentConfigWrites: %w", err)
	}
	defer rows.Close()
	var out []AgentConfigWriteRow
	for rows.Next() {
		var r AgentConfigWriteRow
		var at, keys string
		var created int
		if err := rows.Scan(&r.ID, &at, &r.Tool, &r.FileID, &r.FilePath, &r.Layer, &r.CommonKey, &r.Origin, &r.Mode, &keys,
			&r.BeforeSHA256, &r.AfterSHA256, &r.BackupPath, &created, &r.Warnings); err != nil {
			return nil, fmt.Errorf("store.ListAgentConfigWrites: scan: %w", err)
		}
		r.At, _ = time.Parse(time.RFC3339Nano, at)
		r.Created = created != 0
		_ = json.Unmarshal([]byte(keys), &r.ChangedKeys)
		out = append(out, r)
	}
	return out, rows.Err()
}

// agentConfigKeys keeps a nil key list a JSON array.
func agentConfigKeys(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
