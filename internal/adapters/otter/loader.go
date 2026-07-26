package otter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fallingstar10/craftmake/internal/compiler"
	"gopkg.in/yaml.v3"
)

func LoadLegacy(path string) (*compiler.Context, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read otter config %q: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse otter config %q: %w", path, err)
	}
	normalizeMap(raw)
	baseDirectory := filepath.Dir(path)

	mode := firstString(raw, "workflow.mode", "mode")
	primarySpecies := firstString(raw, "workflow.species.primary", "species1")
	secondarySpecies := firstString(raw, "workflow.species.secondary", "species2")
	jobID := firstString(raw, "workflow.jobid", "jobid")
	userID := firstString(raw, "workflow.userid", "userid")

	sampleIDs := firstStringSlice(raw, "metadata.sids", "workflow.samples", "samples", "sids")
	if len(sampleIDs) == 0 {
		return nil, fmt.Errorf("otter config must define workflow.samples, metadata.SIDs, samples, or SIDs")
	}

	fastqDirectory := firstString(raw, "input.fastq_dir", "output.raw_dir", "directories.raw_dir")
	suffix1 := firstString(raw, "input.suffix", "suffix")
	suffix2 := firstString(raw, "input.suffix2", "suffix2")
	if suffix1 == "" {
		suffix1 = "_R1.fastq.gz"
	}
	if suffix2 == "" {
		suffix2 = strings.Replace(suffix1, "R1", "R2", 1)
	}

	adapter1 := firstStringSlice(raw, "workflow.adapters.seq1", "adapters.adapter1", "trimseq1")
	adapter2 := firstStringSlice(raw, "workflow.adapters.seq2", "adapters.adapter2", "trimseq2")
	samples := make([]compiler.SampleContext, 0, len(sampleIDs))
	for sampleIndex, sampleID := range sampleIDs {
		samples = append(samples, compiler.SampleContext{
			ID:       sampleID,
			Index:    sampleIndex,
			Read1:    resolvePath(baseDirectory, fastqDirectory, sampleID+suffix1),
			Read2:    resolvePath(baseDirectory, fastqDirectory, sampleID+suffix2),
			Adapter1: indexedOrDefault(adapter1, sampleIndex, "NO_ADAPTER_CAL_USE_DEFAULT"),
			Adapter2: indexedOrDefault(adapter2, sampleIndex, "NO_ADAPTER_CAL_USE_DEFAULT"),
		})
	}

	speciesNames := nonEmpty(primarySpecies, secondarySpecies)
	if len(speciesNames) == 0 {
		speciesNames = firstStringSlice(raw, "workflow.species.name", "species")
	}
	if len(speciesNames) == 0 {
		return nil, fmt.Errorf("otter config must define at least one species")
	}
	genomeFasta := firstStringSlice(raw, "reference.files.fasta", "reference.genome_fasta", "genome_fasta")
	genomeIndex := firstStringSlice(raw, "reference.indices.genome", "reference.genome_index", "genomefile")
	rnaGTF := flexibleStringSlice(raw, "reference.rnaseq.gtf", "reference.rnaseq_gtf", "rnaseq_gtf")
	rnaReference := flexibleStringSlice(raw, "reference.rnaseq.ref", "reference.rnaseq.reference", "reference.rnaseq_ref", "rnaseq_ref")
	species := make([]compiler.SpeciesContext, 0, len(speciesNames))
	for speciesIndex, speciesName := range speciesNames {
		role := "primary"
		if speciesIndex > 0 {
			role = "secondary"
		}
		species = append(species, compiler.SpeciesContext{
			Name:            speciesName,
			Index:           speciesIndex,
			Role:            role,
			GenomeFasta:     resolveConfiguredPath(baseDirectory, indexedOrDefault(genomeFasta, speciesIndex, "")),
			GenomeIndex:     resolveConfiguredPath(baseDirectory, indexedOrDefault(genomeIndex, speciesIndex, "")),
			RNASeqGTF:       resolveConfiguredPath(baseDirectory, indexedOrDefault(rnaGTF, speciesIndex, "")),
			RNASeqReference: resolveConfiguredPath(baseDirectory, indexedOrDefault(rnaReference, speciesIndex, "")),
		})
	}

	workflowName := "BeaverBS"
	if strings.EqualFold(mode, "RNASEQ") {
		workflowName = "BeaverRNA"
	}
	if len(species) > 1 {
		if strings.EqualFold(mode, "RNASEQ") {
			workflowName = "BeaverRNASEQPDX"
		} else {
			workflowName = "BeaverPDX"
		}
	}

	canonical := buildCanonicalConfig(raw, baseDirectory)
	setCanonicalPDXReferences(canonical, species)
	setCanonicalRNAReferences(canonical, species, primarySpecies)
	return &compiler.Context{
		Raw: canonical,
		Workflow: compiler.WorkflowContext{
			Mode: mode, WorkflowName: workflowName, JobID: jobID, UserID: userID, PDXMode: len(species) > 1,
		},
		Samples: samples,
		Species: species,
		Paths: map[string]string{
			"config":  path,
			"project": baseDirectory,
		},
	}, nil
}

func buildCanonicalConfig(raw map[string]any, baseDirectory string) map[string]any {
	canonical := cloneMap(raw)
	output := ensureMap(canonical, "output")
	setDefault(output, "raw_dir", firstString(raw, "output.raw_dir", "input.fastq_dir"))
	setDefault(output, "trim_dir", firstString(raw, "output.trim_dir", "directories.trimdir"))
	setDefault(output, "workflow_dir", firstString(raw, "output.workflow_dir", "directories.workflowdir"))
	setDefault(output, "analysis_dir", firstString(raw, "output.analysis_dir", "directories.analysisdir"))
	directories := ensureMap(canonical, "directories")
	setDefault(directories, "sid_log", firstString(raw, "directories.sid_log", "output.log_dir"))
	setDefault(directories, "methylation_call", firstString(raw, "directories.methylation_call", "directories.outdir_mcall"))
	setDefault(directories, "qualimap", firstString(raw, "directories.qualimap", "directories.outdir_qualimap"))
	setDefault(directories, "qc_summary", firstString(raw, "directories.qc_summary", "directories.qcsummary"))
	setDefault(directories, "beta_matrix", firstString(raw, "directories.beta_matrix", "directories.outdir_betam"))
	normalizeRuntimePaths(canonical)
	canonical["config_dir"] = filepath.Clean(baseDirectory)
	return canonical
}

func normalizeRuntimePaths(configuration map[string]any) {
	for _, path := range []string{
		"input.fastq_dir",
		"input.pdata_file",
		"output.base_dir",
		"output.project_dir",
		"output.config_dir",
		"output.data_dir",
		"output.raw_dir",
		"output.trim_dir",
		"output.workflow_dir",
		"output.analysis_dir",
		"output.log_dir",
		"reference.files.fasta",
		"reference.files.genome",
		"reference.files.cgi",
		"reference.files.cpg_sites",
		"reference.indices.genome",
		"reference.genome_fasta",
		"reference.genome_index",
		"reference.rnaseq.gtf",
		"reference.rnaseq.ref",
		"reference.rnaseq.reference",
		"reference.rnaseq_gtf",
		"reference.rnaseq_ref",
	} {
		normalizePathAt(configuration, path)
	}
	if directories, ok := configuration["directories"].(map[string]any); ok {
		normalizePathTree(directories)
	}
}

func normalizePathAt(configuration map[string]any, path string) {
	segments := strings.Split(path, ".")
	current := configuration
	for _, segment := range segments[:len(segments)-1] {
		nested, ok := current[segment].(map[string]any)
		if !ok {
			return
		}
		current = nested
	}
	leaf := segments[len(segments)-1]
	if value, exists := current[leaf]; exists {
		current[leaf] = normalizePathValue(value)
	}
}

func normalizePathTree(value map[string]any) {
	for key, child := range value {
		switch typedChild := child.(type) {
		case map[string]any:
			normalizePathTree(typedChild)
		default:
			value[key] = normalizePathValue(typedChild)
		}
	}
}

func normalizePathValue(value any) any {
	switch typedValue := value.(type) {
	case string:
		return cleanConfiguredPath(typedValue)
	case []any:
		normalizedValues := make([]any, len(typedValue))
		for valueIndex, item := range typedValue {
			normalizedValues[valueIndex] = normalizePathValue(item)
		}
		return normalizedValues
	case []string:
		normalizedValues := make([]string, len(typedValue))
		for valueIndex, item := range typedValue {
			normalizedValues[valueIndex] = cleanConfiguredPath(item)
		}
		return normalizedValues
	default:
		return value
	}
}

func cleanConfiguredPath(configuredPath string) string {
	trimmedPath := strings.TrimSpace(configuredPath)
	if trimmedPath == "" {
		return ""
	}
	portablePath := strings.ReplaceAll(trimmedPath, "\\", "/")
	return filepath.Clean(filepath.FromSlash(portablePath))
}

func normalizeMap(value map[string]any) {
	for key, child := range value {
		normalizedKey := strings.ToLower(key)
		if normalizedKey != key {
			delete(value, key)
			value[normalizedKey] = child
		}
		switch typedChild := child.(type) {
		case map[string]any:
			normalizeMap(typedChild)
		case map[any]any:
			converted := make(map[string]any, len(typedChild))
			for rawKey, rawValue := range typedChild {
				converted[fmt.Sprint(rawKey)] = rawValue
			}
			normalizeMap(converted)
			value[normalizedKey] = converted
		}
	}
}

func lookup(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, segment := range strings.Split(strings.ToLower(path), ".") {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func firstString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		if value, ok := lookup(root, path); ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func firstStringSlice(root map[string]any, paths ...string) []string {
	for _, path := range paths {
		if value, ok := lookup(root, path); ok {
			if result := toStringSlice(value); len(result) > 0 {
				return result
			}
		}
	}
	return nil
}

func flexibleStringSlice(root map[string]any, paths ...string) []string {
	return firstStringSlice(root, paths...)
}

func toStringSlice(value any) []string {
	switch typedValue := value.(type) {
	case []any:
		result := make([]string, 0, len(typedValue))
		for _, item := range typedValue {
			switch typedItem := item.(type) {
			case map[string]any:
				if name, exists := typedItem["name"]; exists {
					result = append(result, fmt.Sprint(name))
				}
			default:
				result = append(result, fmt.Sprint(item))
			}
		}
		return result
	case []string:
		return typedValue
	case string:
		if typedValue != "" {
			return []string{typedValue}
		}
	}
	return nil
}

func indexedOrDefault(values []string, index int, fallback string) string {
	if index >= 0 && index < len(values) && values[index] != "" {
		return values[index]
	}
	if len(values) == 1 && values[0] != "" {
		return values[0]
	}
	return fallback
}

func resolvePath(baseDirectory, directory, name string) string {
	if directory == "" {
		return name
	}
	return resolveConfiguredPath(baseDirectory, filepath.Join(directory, name))
}

func resolveConfiguredPath(baseDirectory, configuredPath string) string {
	normalizedPath := cleanConfiguredPath(configuredPath)
	if normalizedPath == "" || filepath.IsAbs(normalizedPath) {
		return normalizedPath
	}
	return filepath.Clean(filepath.Join(baseDirectory, normalizedPath))
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		if child, ok := value.(map[string]any); ok {
			result[key] = cloneMap(child)
		} else {
			result[key] = value
		}
	}
	return result
}

func ensureMap(root map[string]any, key string) map[string]any {
	if existing, ok := root[key].(map[string]any); ok {
		return existing
	}
	created := make(map[string]any)
	root[key] = created
	return created
}

func setCanonicalPDXReferences(canonical map[string]any, species []compiler.SpeciesContext) {
	graftName := firstString(canonical, "workflow.species.graft", "workflow.species.primary")
	hostName := firstString(canonical, "workflow.species.host", "workflow.species.secondary")
	if graftName == "" && len(species) == 1 {
		graftName = species[0].Name
	}
	reference := ensureMap(canonical, "reference")
	for _, speciesContext := range species {
		switch speciesContext.Name {
		case graftName:
			reference["graft_fasta"] = speciesContext.GenomeFasta
			reference["graft_index"] = speciesContext.GenomeIndex
		case hostName:
			reference["host_fasta"] = speciesContext.GenomeFasta
			reference["host_index"] = speciesContext.GenomeIndex
		}
	}
}

func setCanonicalRNAReferences(canonical map[string]any, species []compiler.SpeciesContext, primarySpecies string) {
	if primarySpecies == "" && len(species) > 0 {
		primarySpecies = species[0].Name
	}
	graftSpecies := firstString(canonical, "workflow.species.graft", "workflow.species.primary")
	hostSpecies := firstString(canonical, "workflow.species.host", "workflow.species.secondary")
	if graftSpecies == "" {
		graftSpecies = primarySpecies
	}

	reference := ensureMap(canonical, "reference")
	rnaReference := ensureMap(reference, "rnaseq")
	for _, speciesContext := range species {
		if speciesContext.Name == primarySpecies {
			setDefault(rnaReference, "primary_gtf", speciesContext.RNASeqGTF)
			setDefault(rnaReference, "primary_reference", speciesContext.RNASeqReference)
		}
		if speciesContext.Name == graftSpecies {
			setDefault(rnaReference, "graft_gtf", speciesContext.RNASeqGTF)
			setDefault(rnaReference, "graft_reference", speciesContext.RNASeqReference)
		}
		if speciesContext.Name == hostSpecies {
			setDefault(rnaReference, "host_gtf", speciesContext.RNASeqGTF)
			setDefault(rnaReference, "host_reference", speciesContext.RNASeqReference)
		}
	}
}

func setDefault(target map[string]any, key, value string) {
	if _, exists := target[key]; !exists && value != "" {
		target[key] = value
	}
}
