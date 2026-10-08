// Package skills keeps the manual recall workflow in the same build as the CLI.
package skills

import _ "embed"

//go:embed mss/SKILL.md
var English string

//go:embed mss/SKILL.zh-CN.md
var Chinese string

//go:embed mss/agents/openai.yaml
var CodexPolicy string
