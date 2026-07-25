package migrations

import _ "embed"

const CurrentVersion = 5

type Migration struct {
	Version int
	Name    string
	SQL     string
}

//go:embed 001_initial.sql
var InitialSchema string

//go:embed 002_runtime_indexes.sql
var runtimeIndexes string

//go:embed 003_artifact_cache_indexes.sql
var artifactCacheIndexes string

//go:embed 004_resume_lineage.sql
var resumeLineage string

//go:embed 005_cache_decisions.sql
var cacheDecisions string

func All() []Migration {
	return []Migration{
		{Version: 1, Name: "initial schema", SQL: InitialSchema},
		{Version: 2, Name: "runtime query indexes", SQL: runtimeIndexes},
		{Version: 3, Name: "artifact cache indexes", SQL: artifactCacheIndexes},
		{Version: 4, Name: "resume lineage", SQL: resumeLineage},
		{Version: 5, Name: "cache decisions", SQL: cacheDecisions},
	}
}
