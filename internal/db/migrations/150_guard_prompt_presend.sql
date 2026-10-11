-- 150_guard_prompt_presend.sql — "a pre-send prompt lane saw THIS prompt"
-- marks, the dedupe key for the post-hoc capture lane
-- (internal/guard/posthocprompt.go, prompt-guard coverage audit 2026-10-02).
--
-- The hook and proxy prompt lanes judge a prompt BEFORE the transcript holds
-- it; the capture lane judges the same prompt again AFTER the watcher ingests
-- it. A mark lets the capture lane stay silent for exactly that prompt (and
-- only that prompt — a different prompt in the same session is still judged).
--
-- prompt_digest is guard.PromptCaptureDigest: sha256 of the prompt AFTER the
-- default storage scrub and whitespace normalisation, so the pre-send side
-- (raw text) and the capture side (already-scrubbed stored text) compute the
-- same value. It is a one-way digest of scrubbed prompt text — never the text,
-- never a matched value.
--
-- NODE-LOCAL: a digest of a short prompt is guessable, so it NEVER rides the
-- org push wire (forbidden-table sentinel, tests/invariant/privacy_test.go).
-- No paired server migration. Written only by
-- internal/store/guard.go::RecordPromptPreSend, which also prunes rows older
-- than seven days (the capture lane only looks back minutes).

CREATE TABLE IF NOT EXISTS guard_prompt_presend (
    id            INTEGER PRIMARY KEY,
    session_id    TEXT NOT NULL,
    prompt_digest TEXT NOT NULL,
    ts            TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_guard_prompt_presend_key ON guard_prompt_presend(session_id, prompt_digest);
CREATE INDEX IF NOT EXISTS idx_guard_prompt_presend_ts  ON guard_prompt_presend(ts);
