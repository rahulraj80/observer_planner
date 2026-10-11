package mcp

// codeintel_opts_helpers_test.go holds the default tool options the symbol
// tool tests share. They live untagged because codeintel_disabled_test.go
// (which runs in every build) uses them too, while the tool tests that seed
// the codeintel store carry !sbo_omit_codeintel (REVIEW-4 P4-08).

func defaultGetSymbolsOpts() GetSymbolsOptions {
	return GetSymbolsOptions{
		AllowExtensions: []string{"tsx", "ts", "js", "go"},
		MaxResponseKB:   100,
		MaxCallers:      20,
		MaxCallees:      20,
	}
}

func defaultGetRelationsOpts() GetRelationsOptions {
	return GetRelationsOptions{
		AllowExtensions: []string{"ts", "tsx", "go"},
		MaxDepth:        5,
		MaxResults:      100,
	}
}
