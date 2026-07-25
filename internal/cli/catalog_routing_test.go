package cli

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestLoadPlanAutomaticallyRoutesXdxtoolsConfigurations(t *testing.T) {
	repositoryRoot := cliRepositoryRoot(t)
	testCases := []struct {
		name             string
		configuration    string
		phase            string
		expectedWorkflow string
		expectedTasks    int
	}{
		{name: "single species bisulfite", configuration: "testdata/configs/beaverbs-step1.yaml", phase: "step1", expectedWorkflow: "BeaverBS", expectedTasks: 7},
		{name: "multi species bisulfite", configuration: "testdata/configs/beaverpdx-step1.yaml", phase: "step1", expectedWorkflow: "BeaverPDX", expectedTasks: 7},
		{name: "single species RNA", configuration: "testdata/configs/beaverrna-step2.yaml", phase: "step2", expectedWorkflow: "BeaverRNA", expectedTasks: 6},
		{name: "multi species RNA", configuration: "testdata/configs/beaverrnaseqpdx-step3.yaml", phase: "step3", expectedWorkflow: "BeaverRNASEQPDX", expectedTasks: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			options := commonOptions{
				configPath: filepath.Join(repositoryRoot, testCase.configuration),
				catalogDir: filepath.Join(repositoryRoot, "workflows"),
				phase:      testCase.phase,
			}
			plan, err := loadPlan(&options)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Workflow != testCase.expectedWorkflow || plan.Phase != testCase.phase || len(plan.Tasks) != testCase.expectedTasks {
				t.Fatalf("unexpected routed plan: workflow=%s phase=%s tasks=%d", plan.Workflow, plan.Phase, len(plan.Tasks))
			}
			expectedWorkflowPath := filepath.Join(repositoryRoot, "workflows", testCase.expectedWorkflow, testCase.phase+".yaml")
			if options.workflowPath != expectedWorkflowPath {
				t.Fatalf("unexpected discovered workflow path %q, expected %q", options.workflowPath, expectedWorkflowPath)
			}
			if !filepath.IsAbs(options.configPath) || !filepath.IsAbs(options.workflowPath) {
				t.Fatalf("routing should normalize persisted paths: %#v", options)
			}
		})
	}
}

func TestLoadPlanRequiresPhaseWithoutExplicitWorkflow(t *testing.T) {
	repositoryRoot := cliRepositoryRoot(t)
	options := commonOptions{
		configPath: filepath.Join(repositoryRoot, "testdata", "configs", "beaverbs-step1.yaml"),
		catalogDir: filepath.Join(repositoryRoot, "workflows"),
	}
	_, err := loadPlan(&options)
	if err == nil || !strings.Contains(err.Error(), "--phase is required") {
		t.Fatalf("expected missing phase error, got %v", err)
	}
}

func TestLoadPlanReportsMissingCatalogEntry(t *testing.T) {
	repositoryRoot := cliRepositoryRoot(t)
	options := commonOptions{
		configPath: filepath.Join(repositoryRoot, "testdata", "configs", "beaverrna-step1.yaml"),
		catalogDir: t.TempDir(),
		phase:      "step3",
	}
	_, err := loadPlan(&options)
	if err == nil || !strings.Contains(err.Error(), "BeaverRNA/step3.yaml was not found") {
		t.Fatalf("expected missing catalog entry error, got %v", err)
	}
}

func TestLoadPlanRejectsExplicitWorkflowForDifferentConfigurationType(t *testing.T) {
	repositoryRoot := cliRepositoryRoot(t)
	options := commonOptions{
		configPath:   filepath.Join(repositoryRoot, "testdata", "configs", "beaverrna-step1.yaml"),
		workflowPath: filepath.Join(repositoryRoot, "workflows", "BeaverBS", "step1.yaml"),
	}
	_, err := loadPlan(&options)
	if err == nil || !strings.Contains(err.Error(), "config resolves to BeaverRNA") {
		t.Fatalf("expected workflow/config mismatch error, got %v", err)
	}
}

func TestSchedulerFlagDefaultsMatchRunAndResume(t *testing.T) {
	expectedDefaults := map[string]string{
		"workers":                  "0",
		"max-parallel":             strconv.Itoa(runtime.NumCPU()),
		"max-cores":                "0",
		"max-memory":               "0",
		"slurm-submit-attempts":    "8",
		"slurm-submit-backoff":     "1s",
		"slurm-submit-max-backoff": "30s",
		"slurm-pending-timeout":    "0s",
	}
	commands := map[string]*cobra.Command{
		"run":    newRunCommand(BuildInfo{}),
		"resume": newResumeCommand(BuildInfo{}),
	}
	for commandName, command := range commands {
		t.Run(commandName, func(t *testing.T) {
			for flagName, expectedDefault := range expectedDefaults {
				flag := command.Flags().Lookup(flagName)
				if flag == nil {
					t.Fatalf("command %s is missing --%s", commandName, flagName)
				}
				if flag.DefValue != expectedDefault {
					t.Fatalf("unexpected default for %s --%s: got %q, expected %q", commandName, flagName, flag.DefValue, expectedDefault)
				}
			}
		})
	}
}

func TestResolveEffectiveWorkersUsesExplicitWorkersOrCompatibilityFallback(t *testing.T) {
	testCases := []struct {
		name          string
		workers       int
		maxParallel   int
		expected      int
		expectedError string
	}{
		{name: "explicit workers", workers: 5, maxParallel: 12, expected: 5},
		{name: "max parallel fallback", workers: 0, maxParallel: 12, expected: 12},
		{name: "negative workers", workers: -1, maxParallel: 12, expectedError: "--workers must be zero or positive"},
		{name: "nonpositive fallback", workers: 0, maxParallel: 0, expectedError: "--max-parallel must be positive"},
		{name: "invalid fallback is rejected even with explicit workers", workers: 2, maxParallel: 0, expectedError: "--max-parallel must be positive"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := resolveEffectiveWorkers(testCase.workers, testCase.maxParallel)
			if testCase.expectedError == "" {
				if err != nil {
					t.Fatal(err)
				}
				if actual != testCase.expected {
					t.Fatalf("unexpected effective workers: got %d, expected %d", actual, testCase.expected)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
				t.Fatalf("expected error containing %q, got %v", testCase.expectedError, err)
			}
		})
	}
}

func TestEffectiveSchedulerMaxCoresSeparatesLocalAndSlurmDefaults(t *testing.T) {
	if actual := effectiveSchedulerMaxCores("local", 0, false); actual != runtime.NumCPU() {
		t.Fatalf("local default max cores: got %d, expected %d", actual, runtime.NumCPU())
	}
	if actual := effectiveSchedulerMaxCores("slurm", 0, false); actual != 0 {
		t.Fatalf("Slurm default max cores should be unlimited, got %d", actual)
	}
	if actual := effectiveSchedulerMaxCores("local", 3, true); actual != 3 {
		t.Fatalf("explicit local max cores was not preserved: %d", actual)
	}
}

func TestConfiguredSlurmBackendValidatesRetryAndPendingOptions(t *testing.T) {
	configured, err := configuredSlurmBackend("compute", 4, 2*time.Second, 10*time.Second, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if configured.PartitionOverride != "compute" || configured.SubmitMaxAttempts != 4 || configured.SubmitInitialBackoff != 2*time.Second || configured.SubmitMaximumBackoff != 10*time.Second || configured.PendingTimeout != time.Hour {
		t.Fatalf("unexpected configured Slurm backend: %#v", configured)
	}

	testCases := []struct {
		name          string
		attempts      int
		backoff       time.Duration
		maximum       time.Duration
		pending       time.Duration
		expectedError string
	}{
		{name: "nonpositive attempts", attempts: 0, backoff: time.Second, maximum: time.Second, expectedError: "--slurm-submit-attempts must be positive"},
		{name: "nonpositive backoff", attempts: 1, backoff: 0, maximum: time.Second, expectedError: "--slurm-submit-backoff must be positive"},
		{name: "nonpositive maximum", attempts: 1, backoff: time.Second, maximum: 0, expectedError: "--slurm-submit-max-backoff must be positive"},
		{name: "maximum below initial", attempts: 1, backoff: 2 * time.Second, maximum: time.Second, expectedError: "must be at least"},
		{name: "negative pending timeout", attempts: 1, backoff: time.Second, maximum: time.Second, pending: -time.Second, expectedError: "must be zero or positive"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, configureErr := configuredSlurmBackend("", testCase.attempts, testCase.backoff, testCase.maximum, testCase.pending)
			if configureErr == nil || !strings.Contains(configureErr.Error(), testCase.expectedError) {
				t.Fatalf("expected error containing %q, got %v", testCase.expectedError, configureErr)
			}
		})
	}
}

func cliRepositoryRoot(t *testing.T) string {
	t.Helper()
	absoluteRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return absoluteRoot
}
