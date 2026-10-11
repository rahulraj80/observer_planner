//go:build !sbo_omit_cloud_intelligence

package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/cloudevidence"
	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// projects_cloudgrade.go is the commit-alignment envelope input half of
// projects_wire.go, split out because it builds a
// cloudevidence.CommitAlignmentInput and so belongs to cloud.intelligence
// (lane MP-L1I, T-715). A build with -tags sbo_omit_cloud_intelligence
// leaves it out together with cloud_gradecommit.go.

func init() {
	// gradeCommitLoader defaults (cloud_gradecommit.go) to a stub that
	// always fails honestly ("no commit-alignment loader is wired
	// yet"). This is the ONE reassignment to the real implementation,
	// done once at process start (before any command can run) so every
	// caller of the package var (today only `observer cloud
	// grade-commit`'s RunE) sees the real loader. Kept as a package
	// init rather than a call threaded through newCloudCmd() so this
	// wave's cmd-side footprint stays entirely inside this file.
	gradeCommitLoader = realGradeCommitLoader
}

// realGradeCommitLoader loads one prompt's attribution facts
// (internal/store.LoadPromptChainInput, W3) and converts them into a
// cloudevidence.CommitAlignmentInput. It opens its own config + DB (the
// same "no --config flag, resolve the default location" shape
// newCloudGradeCommitCmd's RunE uses, mirroring every other bare
// `observer cloud <verb>` subcommand's loadConfigAndDB(ctx, "") call)
// because the grade-commit command carries no pre-opened store to
// inject through.
func realGradeCommitLoader(ctx context.Context, actionID int64) (cloudevidence.CommitAlignmentInput, error) {
	_, database, cleanup, err := loadConfigAndDB(ctx, "")
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: %w", err)
	}
	defer cleanup()
	st := store.New(database)

	projectID, err := st.ProjectIDForAction(ctx, actionID)
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: %w", err)
	}
	_, facts, err := st.LoadPromptChainInput(ctx, projectID, actionID)
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: %w", err)
	}

	auth, ok, err := st.SessionAuthority(ctx, facts.SessionID)
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: read session authority: %w", err)
	}
	if !ok {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: session %q not found, or its data-authority is unknown", facts.SessionID)
	}

	cloudSession, err := st.GetOrCreateCloudSessionPseudonym(ctx, facts.SessionID)
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: session pseudonym: %w", err)
	}
	cloudProject, err := st.GetOrCreateCloudProjectPseudonym(ctx, strconv.FormatInt(projectID, 10))
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: project pseudonym: %w", err)
	}
	purposes, err := cloudPurposeSet(cloudcontract.PurposeExtendedEvidence)
	if err != nil {
		return cloudevidence.CommitAlignmentInput{}, fmt.Errorf("observer cloud grade-commit: %w", err)
	}

	files := make([]cloudevidence.CommitAlignmentFileInput, 0, len(facts.CommitFiles))
	for _, f := range facts.CommitFiles {
		files = append(files, cloudevidence.CommitAlignmentFileInput{PathHash: f.PathHash, Added: f.Added, Deleted: f.Deleted})
	}
	hunks := make([]cloudevidence.CommitAlignmentHunkInput, 0, len(facts.Hunks))
	for _, h := range facts.Hunks {
		hunks = append(hunks, cloudevidence.CommitAlignmentHunkInput{PathHash: h.PathHash, Excerpt: h.Excerpt})
	}

	return cloudevidence.CommitAlignmentInput{
		CloudProjectID:  cloudProject,
		CloudSessionID:  cloudSession,
		PromptExcerpt:   facts.PromptText,
		CommitSubject:   facts.CommitSubject,
		CommitFiles:     files,
		EditHunks:       hunks,
		LinkStatus:      facts.LinkStatus,
		WindowDays:      facts.WindowDays,
		Authority:       auth,
		GrantedPurposes: purposes,
		Scrubber:        scrub.New(),
		ScrubberVersion: cloudScrubberVersion,
	}, nil
}
