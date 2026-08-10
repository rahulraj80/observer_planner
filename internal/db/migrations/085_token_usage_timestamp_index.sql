-- Migration 085: timestamp index on token_usage.
--
-- windowSpendSince (store.GuardBudgetSpend, §12.1 budget + §4.4 cost-matcher
-- seam) and the briefing/rollup windows filter token_usage on `timestamp`, but
-- only api_turns carried a timestamp index — token_usage was full-scanned
-- (136k rows, pure-Go modernc) three times per guard evaluation (day / 7-day /
-- calendar-month), pushing the 3s budget-lookup deadline into context-deadline
-- timeouts that retried and burned CPU. api_turns.timestamp stays on its own
-- index (001); this restores the symmetry.

CREATE INDEX IF NOT EXISTS idx_token_usage_timestamp ON token_usage(timestamp);
