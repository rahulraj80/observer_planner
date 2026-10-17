package main

import "context"

// benchmark_evalscore.go holds the obs-eval bridge type the benchmark scorer
// takes. It is not in benchmark_score.go because obs_wire.go /
// obs_wire_stub.go name it in every build, including one without the
// benchmark component (lane MP-L1E).

// evalScoreFn runs one reused obs-eval registry scorer over a shaped sample and
// returns plain outputs (no eval types cross this boundary). available=false
// means the scorer could not be built (e.g. llm_judge without a wired judge) ⇒
// the runner records scorer_unavailable, never a silent pass.
type evalScoreFn func(ctx context.Context, scorer string, params map[string]string, input, output, reference string) (score float64, passed bool, rationale string, available bool, err error)
