-- 156_guard_chain_v2.sql - guard_events chain input version 2
-- (request-grants G3; orchestrator ruling: grant_id joins the chain-hash
-- input under a NEW chain-input version so chains written before still
-- verify).
--
-- guard_events is the §10.4 hash-chained audit log. Version 1 (every row
-- written before this migration, and the column default) hashes the 17 row
-- fields documented on store.GuardEventRow.canonicalBytes. Version 2 (every
-- row written after) prefixes the version tag and appends grant_id, so the
-- org grant that turned a deny into an allow-by-grant is tamper-evident like
-- the decision it changed. The verifier reads chain_v per row and recomputes
-- with that version's preimage, so a mixed chain verifies end to end; moving
-- a v2 row back to v1 changes its recomputed hash and is reported as a
-- rewritten row.
--
-- NODE-LOCAL column: the org push does not select it (the org stores
-- grant_id itself, migration org 196 / pg 0062) and the org does not
-- re-verify node chains. NO server pair.

ALTER TABLE guard_events ADD COLUMN chain_v INTEGER NOT NULL DEFAULT 1;
