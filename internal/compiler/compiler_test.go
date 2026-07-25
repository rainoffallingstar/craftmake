package compiler

import (
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestParseMemory(t *testing.T) {
	value, err := ParseMemory("1.5G")
	if err != nil {
		t.Fatal(err)
	}
	if value != 1610612736 {
		t.Fatalf("unexpected bytes: %d", value)
	}
}

func TestRenderRejectsUnknownPath(t *testing.T) {
	_, err := Render("${{ sample.missing }}", map[string]any{"sample": map[string]any{"id": "S1"}})
	if err == nil {
		t.Fatal("expected render error")
	}
}

func TestCompileRejectsBatchAllocationWithInsufficientEffectiveCores(t *testing.T) {
	workflow := batchWorkflow(spec.ResourceSpec{Cores: 2, Memory: "256M"}, spec.ResourceSpec{Cores: 1, Memory: "64M"}, 4)
	_, err := Compile(workflow, batchContext(3))
	if err == nil || !strings.Contains(err.Error(), "allocation cores") {
		t.Fatalf("expected batch core capacity error, received %v", err)
	}
}

func TestCompileRejectsBatchAllocationWithInsufficientEffectiveMemory(t *testing.T) {
	workflow := batchWorkflow(spec.ResourceSpec{Cores: 4, Memory: "128M"}, spec.ResourceSpec{Cores: 1, Memory: "64M"}, 4)
	_, err := Compile(workflow, batchContext(3))
	if err == nil || !strings.Contains(err.Error(), "allocation memory") {
		t.Fatalf("expected batch memory capacity error, received %v", err)
	}
}

func TestCompileAllowsBatchAllocationWhenEffectiveParallelismFits(t *testing.T) {
	workflow := batchWorkflow(spec.ResourceSpec{Cores: 2, Memory: "128M"}, spec.ResourceSpec{Cores: 1, Memory: "64M"}, 4)
	plan, err := Compile(workflow, batchContext(2))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Submissions) != 1 || len(plan.Submissions[0].TaskIDs) != 2 {
		t.Fatalf("unexpected batch submissions: %#v", plan.Submissions)
	}
}

func TestCompileUsesDefaultAndJobLogCompressionSettings(t *testing.T) {
	defaultCompression := false
	jobCompression := true
	workflow := batchWorkflow(spec.ResourceSpec{Cores: 2, Memory: "128M"}, spec.ResourceSpec{Cores: 1, Memory: "64M"}, 2)
	workflow.Defaults.Observability.CompressSuccessLogs = &defaultCompression
	job := workflow.Jobs["process"]
	job.Observability.CompressSuccessLogs = &jobCompression
	workflow.Jobs["process"] = job

	plan, err := Compile(workflow, batchContext(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range plan.Tasks {
		if !task.CompressSuccessLogs {
			t.Fatalf("job observability setting did not override defaults for task %s", task.ID)
		}
	}
}

func TestCompileDefaultsToCompressingSuccessfulLogs(t *testing.T) {
	workflow := batchWorkflow(spec.ResourceSpec{Cores: 2, Memory: "128M"}, spec.ResourceSpec{Cores: 1, Memory: "64M"}, 2)
	plan, err := Compile(workflow, batchContext(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range plan.Tasks {
		if !task.CompressSuccessLogs {
			t.Fatalf("successful log compression should default to enabled for task %s", task.ID)
		}
	}
}

func batchWorkflow(allocation, worker spec.ResourceSpec, maxParallel int) *spec.WorkflowSpec {
	return &spec.WorkflowSpec{
		Name:     "Batch resource test",
		Version:  spec.CurrentVersion,
		On:       spec.TriggerSpec{Xdxtools: spec.XdxtoolsTrigger{Workflow: "Smoke", Phase: "batch", Modes: []string{"RRBS"}}},
		Defaults: spec.DefaultsSpec{Shell: "bash"},
		Jobs: map[string]spec.JobSpec{
			"process": {
				Scope:      "batch",
				Dimensions: []string{"sample"},
				Resources:  allocation,
				Worker:     &spec.WorkerSpec{Resources: worker, MaxParallel: maxParallel},
				Steps:      []spec.StepSpec{{Name: "noop", Run: "true"}},
			},
		},
	}
}

func batchContext(sampleCount int) *Context {
	samples := make([]SampleContext, sampleCount)
	for sampleIndex := range samples {
		samples[sampleIndex] = SampleContext{ID: "sample-" + string(rune('A'+sampleIndex)), Index: sampleIndex}
	}
	return &Context{
		Raw:      map[string]any{},
		Workflow: WorkflowContext{Mode: "RRBS"},
		Samples:  samples,
	}
}
