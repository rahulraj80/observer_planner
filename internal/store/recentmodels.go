package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecentToolModel is one recently-used model for a tool, aggregated from
// token_usage — the store-side half of the New Terminal model picker (B5).
// Count is the number of token_usage rows observed for the model within the
// query window; LastUsed is the most recent row's timestamp, formatted
// RFC3339 (the same string shape token_usage.timestamp is stored as).
type RecentToolModel struct {
	Model    string
	Count    int
	LastUsed string
}

// LoadRecentModelsForTool returns the models used by tool in the last
// window, most-recently-used first, capped at limit. It excludes rows with
// no model and sentinel placeholder models (e.g. "<synthetic>", stored with
// a leading '<' — never a real model identifier). Used to seed the New
// Terminal model picker's "recent" suggestions (B5) — see
// internal/intelligence/dashboard's modelSuggestionsFor, which combines this
// history with the tool's grounded ModelSpec.Known examples.
func (s *Store) LoadRecentModelsForTool(ctx context.Context, tool string, window time.Duration, limit int) ([]RecentToolModel, error) {
	// token_usage.timestamp rows are stored RFC3339Nano (see store.go's
	// timestamp helper); format the cutoff the same way so the string
	// comparison sql performs is never skewed by a shorter suffix.
	since := time.Now().UTC().Add(-window).Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, COUNT(*), MAX(timestamp)
		  FROM token_usage
		 WHERE tool = ?
		   AND model IS NOT NULL AND model != ''
		   AND model NOT LIKE '<%'
		   AND timestamp >= ?
		 GROUP BY model
		 ORDER BY MAX(timestamp) DESC
		 LIMIT ?`, tool, since, limit)
	if err != nil {
		return nil, fmt.Errorf("store.LoadRecentModelsForTool: %w", err)
	}
	defer rows.Close()

	var out []RecentToolModel
	for rows.Next() {
		var m RecentToolModel
		if err := rows.Scan(&m.Model, &m.Count, &m.LastUsed); err != nil {
			return nil, fmt.Errorf("store.LoadRecentModelsForTool: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.LoadRecentModelsForTool: rows: %w", err)
	}
	return out, nil
}

// ModelUsage is one model this node has used, aggregated over token_usage:
// Count is the number of token_usage rows (model turns) in the window,
// LastUsed the most recent row's timestamp (RFC3339, as stored) and Tools the
// distinct tools that ran it, sorted. It feeds the Arena model pickers'
// "used on this machine" group.
type ModelUsage struct {
	Model    string
	Count    int
	LastUsed string
	Tools    []string
}

// LoadModelUsage returns the models used within window, MOST-USED first
// (turn count descending, then most recent, then model id), capped at limit.
// tool == "" aggregates every tool on the node; otherwise only that tool's
// rows count. Rows with no model and sentinel placeholders (a leading '<',
// e.g. "<synthetic>") are excluded, exactly as LoadRecentModelsForTool does.
func (s *Store) LoadModelUsage(ctx context.Context, tool string, window time.Duration, limit int) ([]ModelUsage, error) {
	since := time.Now().UTC().Add(-window).Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, COUNT(*), MAX(timestamp), COALESCE(GROUP_CONCAT(DISTINCT tool), '')
		  FROM token_usage
		 WHERE (? = '' OR tool = ?)
		   AND model IS NOT NULL AND model != ''
		   AND model NOT LIKE '<%'
		   AND timestamp >= ?
		 GROUP BY model
		 ORDER BY COUNT(*) DESC, MAX(timestamp) DESC, model ASC
		 LIMIT ?`, tool, tool, since, limit)
	if err != nil {
		return nil, fmt.Errorf("store.LoadModelUsage: %w", err)
	}
	defer rows.Close()

	var out []ModelUsage
	for rows.Next() {
		var (
			m     ModelUsage
			tools string
		)
		if err := rows.Scan(&m.Model, &m.Count, &m.LastUsed, &tools); err != nil {
			return nil, fmt.Errorf("store.LoadModelUsage: scan: %w", err)
		}
		for _, t := range strings.Split(tools, ",") {
			if t = strings.TrimSpace(t); t != "" {
				m.Tools = append(m.Tools, t)
			}
		}
		sort.Strings(m.Tools)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.LoadModelUsage: rows: %w", err)
	}
	return out, nil
}
