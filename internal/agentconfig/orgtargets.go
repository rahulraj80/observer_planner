package agentconfig

// Target files for the organization's policy (phase P6, plan sections 3.2
// and 6.3): where an org value for one setting lands on a managed node.

// UserFileFor returns the user-scope file row id an org (or L1 / L2) value
// of a setting is written to, or "" when the tool has no user file Observer
// may write for it.
func UserFileFor(settingID string) string { return userFileFor(settingID) }

// ProjectFileFor returns the project-scope file row id a project value of a
// setting is written to: with localOnly, the vendor's personal (gitignored)
// project file; otherwise the shared project file a commit carries. "" when
// the setting has no such file (the org lint refuses that placement).
func ProjectFileFor(settingID string, localOnly bool) string {
	r, ok := Setting(settingID)
	if !ok {
		return ""
	}
	want := ScopeProjectShared
	if localOnly {
		want = ScopeProjectLocal
	}
	if !scopeAllowed(r.Scopes, want) {
		return ""
	}
	for _, f := range filesTable() {
		if f.Shape != r.FileID || f.Scope != want || f.WriteNeeds != PrivUser {
			continue
		}
		if _, ok := f.Format.Engine(); !ok {
			continue
		}
		if !localOnly && f.Gitignored {
			continue
		}
		return f.ID
	}
	return ""
}

// ProjectFiles returns every writable project-scope file row of a setting's
// shape (shared and local), for the shadow check: an enforced user value is
// also enforced in any project file that overrides it (D-124).
func ProjectFiles(settingID string) []string {
	r, ok := Setting(settingID)
	if !ok {
		return nil
	}
	var out []string
	for _, f := range filesTable() {
		if f.Shape != r.FileID || f.WriteNeeds != PrivUser {
			continue
		}
		if f.Scope != ScopeProjectShared && f.Scope != ScopeProjectLocal {
			continue
		}
		if _, ok := f.Format.Engine(); ok {
			out = append(out, f.ID)
		}
	}
	return out
}

// TrackedShadow reports whether an ENFORCED user-level value of a setting may
// be written into a tracked project file on a clone: the tool reads the key
// from a shared project file a commit carries, and no local-only project file
// wins over that file to shadow it instead (T-431, D-103). The publish lint
// warns the admin with it; the node applies the same rule per clone.
func TrackedShadow(settingID string) bool {
	r, ok := Setting(settingID)
	if !ok {
		return false
	}
	tracked, local := -1, -1
	for _, f := range filesTable() {
		if f.Shape != r.FileID || f.WriteNeeds != PrivUser {
			continue
		}
		if _, ok := f.Format.Engine(); !ok {
			continue
		}
		switch {
		case f.Scope == ScopeProjectShared && !f.Gitignored:
			if f.Precedence > tracked {
				tracked = f.Precedence
			}
		case f.Scope == ScopeProjectLocal || f.Gitignored:
			if f.Precedence > local {
				local = f.Precedence
			}
		}
	}
	return tracked >= 0 && local <= tracked
}

// IsInstructionKind reports a Markdown instruction-family file kind.
func IsInstructionKind(k FileKind) bool { return isInstructionKind(k) }
