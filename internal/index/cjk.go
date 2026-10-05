package index

import "github.com/henryyu333/mss/internal/query"

// The CJK tokenisation moved to query, which index and prompt both sit on.
// This keeps the in-package name the ingestion path and its tests use.
func expandCJKTokens(toks []string) []string { return query.ExpandCJKTokens(toks) }
