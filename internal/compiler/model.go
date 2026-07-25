package compiler

import (
	"github.com/fallingstar10/craftmake/internal/spec"
	"github.com/fallingstar10/craftmake/pkg/protocol"
)

type Context struct {
	Raw      map[string]any
	Workflow WorkflowContext
	Samples  []SampleContext
	Species  []SpeciesContext
	Paths    map[string]string
}

type WorkflowContext struct {
	Mode         string
	WorkflowName string
	JobID        string
	UserID       string
	PDXMode      bool
}

type SampleContext struct {
	ID       string
	Index    int
	Read1    string
	Read2    string
	Adapter1 string
	Adapter2 string
}

type SpeciesContext struct {
	Name            string
	Index           int
	Role            string
	GenomeFasta     string
	GenomeIndex     string
	RNASeqGTF       string
	RNASeqReference string
}

type Task struct {
	ID                  string
	JobID               string
	JobName             string
	Workflow            string
	Phase               string
	Scope               string
	Dimensions          map[string]string
	Inputs              map[string][]string
	Outputs             map[string]string
	Resources           protocol.ResourceRequest
	Worker              *WorkerPlan
	Environment         string
	Env                 map[string]string
	Steps               []protocol.StepManifest
	Dependencies        []string
	MaxAttempts         int
	CompressSuccessLogs bool
	Fingerprint         string
}

type WorkerPlan struct {
	Resources   protocol.ResourceRequest
	MaxParallel int
}

type SubmissionGroup struct {
	ID        string
	Scope     string
	GroupKey  string
	TaskIDs   []string
	Resources protocol.ResourceRequest
	Worker    *WorkerPlan
}

type Plan struct {
	Workflow    string
	Phase       string
	Name        string
	Tasks       []*Task
	TaskByID    map[string]*Task
	Order       []string
	Submissions []SubmissionGroup
	Source      *spec.WorkflowSpec
}
