package compiler_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverBSStep1Fixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverBS", "step1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverbs-step1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 7 || len(plan.Submissions) != 7 {
		t.Fatalf("expected seven tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}
	if len(plan.Order) != 7 {
		t.Fatalf("expected seven tasks in topological order, got %d", len(plan.Order))
	}

	fastqcBeforeSampleA := plan.TaskByID["BeaverBS/step1/fastqc_before/sample=sample-a"]
	if fastqcBeforeSampleA == nil {
		t.Fatal("missing sample-a fastqc_before task")
	}
	if len(fastqcBeforeSampleA.Dependencies) != 0 {
		t.Fatalf("fastqc_before should have no dependencies: %#v", fastqcBeforeSampleA.Dependencies)
	}

	fastqcAfterSampleA := plan.TaskByID["BeaverBS/step1/fastqc_after/sample=sample-a"]
	if fastqcAfterSampleA == nil || !containsTaskID(fastqcAfterSampleA.Dependencies, "BeaverBS/step1/trim_reads/sample=sample-a") {
		t.Fatalf("fastqc_after should depend on sample-a trimming: %#v", fastqcAfterSampleA)
	}

	checker := plan.TaskByID["BeaverBS/step1/step1_checker"]
	if checker == nil {
		t.Fatal("missing global step1 checker")
	}
	if len(checker.Dependencies) != 6 {
		t.Fatalf("checker should depend on six sample tasks, got %d: %#v", len(checker.Dependencies), checker.Dependencies)
	}
	if len(checker.Inputs["trimmed_read1"]) != 2 || len(checker.Inputs["after_read2"]) != 2 {
		t.Fatalf("checker should aggregate both samples: %#v", checker.Inputs)
	}
	if checker.Outputs["success_marker"] != filepath.Join("workflow", "log", "step1_success.txt") {
		t.Fatalf("unexpected checker marker path %q", checker.Outputs["success_marker"])
	}
	if !strings.Contains(plan.TaskByID["BeaverBS/step1/trim_reads/sample=sample-a"].Steps[0].Command, "trim_galore") {
		t.Fatal("trim task command does not contain trim_galore")
	}
}

func containsTaskID(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func repositoryRootForTest(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}
