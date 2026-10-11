-- 161_update_state_distro.sql - modular packaging P3 remainder (lane
-- MP-REL2, T-687; plan 5.6.1 "Node migration" and the fourth-pass M8
-- legacy defaults; the next contiguous agent number per D-417).
--
-- WHY. A distro-aware node must remember three facts across restarts to
-- take a v2 update manifest safely:
--   current_distro     the prebuilt distro the running binary was built as.
--                      Backfilled to 'full': every pre-modular binary is the
--                      full build (nodecomponents.LegacyDefaults owns that
--                      meaning). A stamped binary reports its own distro;
--                      the column records what the last successful apply
--                      installed.
--   components_digest  components.Digest of the installed binary's compiled
--                      set, '' until the first apply records it (the running
--                      binary's own digest is read from the binary itself,
--                      never trusted from this column).
--   distro_epochs      a JSON object {channel: epoch}: the distro_epoch of
--                      the last distro change applied per channel. A v2
--                      manifest may change the distro only with an epoch
--                      ahead of this one (update.Verify rule 12). '{}' means
--                      epoch 0 on every channel. The epoch domain includes
--                      the channel, so switching channel resets nothing.
--
-- ONE WRITER. Only the update apply path writes them, in the same state
-- transition that records a successful handshake (store/update.go
-- SaveUpdateState via TransitionUpdateState); a rolled-back apply keeps the
-- old distro and epoch.
--
-- NODE-LOCAL. update_state is in tests/invariant/privacy_test.go's
-- forbidden-name set (migration 105), so additive columns need no privacy
-- edit and the org wire carries none of them. NO server pair. Additive with
-- defaults: a binary from before 161 never names the columns, and a newer
-- binary on a database not yet at 161 reads them as the legacy defaults
-- (store.LoadUpdateState tolerates their absence).

ALTER TABLE update_state ADD COLUMN current_distro TEXT NOT NULL DEFAULT 'full';
ALTER TABLE update_state ADD COLUMN components_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE update_state ADD COLUMN distro_epochs TEXT NOT NULL DEFAULT '{}';
