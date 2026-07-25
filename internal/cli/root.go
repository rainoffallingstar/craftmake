package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/backend"
	"github.com/fallingstar10/craftmake/internal/backend/local"
	"github.com/fallingstar10/craftmake/internal/backend/slurm"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/controllerlog"
	"github.com/fallingstar10/craftmake/internal/report"
	runtimeexecutor "github.com/fallingstar10/craftmake/internal/runtime"
	"github.com/fallingstar10/craftmake/internal/scheduler"
	"github.com/fallingstar10/craftmake/internal/spec"
	"github.com/fallingstar10/craftmake/internal/store"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type commonOptions struct {
	workflowPath string
	configPath   string
	projectDir   string
	stateDir     string
	catalogDir   string
	phase        string
	format       string
}

func NewRootCommand(buildInfo BuildInfo) *cobra.Command {
	root := &cobra.Command{
		Use:           "craftmake",
		Short:         "Native workflow runner for xdxtools pipelines",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s+%s (%s)", buildInfo.Version, buildInfo.Commit, buildInfo.Date),
	}
	commands := []*cobra.Command{newValidateCommand(), newPlanCommand(), newRunCommand(buildInfo), newStatusCommand(), newCancelCommand(), newReportCommand(), newLogsCommand(), newDoctorCommand(), newResumeCommand(buildInfo), newTaskRunnerCommand()}
	for _, command := range commands {
		if command.Args == nil {
			command.Args = noArguments
		}
		command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
			return WithExitCode(ExitUsage, err)
		})
	}
	root.AddCommand(commands...)
	return root
}

func noArguments(command *cobra.Command, arguments []string) error {
	if len(arguments) == 0 {
		return nil
	}
	return usageError("command %q does not accept positional arguments", command.CommandPath())
}

func newValidateCommand() *cobra.Command {
	options := commonOptions{}
	command := &cobra.Command{Use: "validate", Short: "Validate workflow and configuration", RunE: func(command *cobra.Command, arguments []string) error {
		plan, err := loadPlan(&options)
		if err != nil {
			return err
		}
		fmt.Fprintf(command.OutOrStdout(), "valid: %s %s (%d tasks, %d submissions)\n", plan.Workflow, plan.Phase, len(plan.Tasks), len(plan.Submissions))
		return nil
	}}
	addPlanFlags(command, &options)
	return command
}

func newPlanCommand() *cobra.Command {
	options := commonOptions{format: "table"}
	command := &cobra.Command{Use: "plan", Short: "Compile and display the task DAG", RunE: func(command *cobra.Command, arguments []string) error {
		plan, err := loadPlan(&options)
		if err != nil {
			return err
		}
		if options.format == "json" {
			encoder := json.NewEncoder(command.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(plan)
		}
		printPlan(command, plan)
		return nil
	}}
	addPlanFlags(command, &options)
	command.Flags().StringVar(&options.format, "format", "table", "Output format (table/json)")
	return command
}

func newRunCommand(buildInfo BuildInfo) *cobra.Command {
	options := commonOptions{}
	var backendName string
	var slurmPartition string
	var workers int
	var maxParallel int
	var maxCores int
	var maxMemory string
	var slurmSubmitAttempts int
	var slurmSubmitBackoff time.Duration
	var slurmSubmitMaximumBackoff time.Duration
	var slurmPendingTimeout time.Duration
	var force bool
	var dryRun bool
	var runID string
	command := &cobra.Command{Use: "run", Short: "Run a compiled workflow", RunE: func(command *cobra.Command, arguments []string) error {
		plan, err := loadPlan(&options)
		if err != nil {
			return err
		}
		if dryRun {
			printPlan(command, plan)
			return nil
		}
		effectiveWorkers, workerErr := resolveEffectiveWorkers(workers, maxParallel)
		if workerErr != nil {
			return workerErr
		}
		effectiveMaxCores := effectiveSchedulerMaxCores(backendName, maxCores, command.Flags().Changed("max-cores"))
		var selectedBackend backend.Backend
		switch backendName {
		case "local":
			selectedBackend = local.New()
		case "slurm":
			slurmBackend, configureErr := configuredSlurmBackend(
				slurmPartition,
				slurmSubmitAttempts,
				slurmSubmitBackoff,
				slurmSubmitMaximumBackoff,
				slurmPendingTimeout,
			)
			if configureErr != nil {
				return configureErr
			}
			selectedBackend = slurmBackend
		default:
			return usageError("unsupported backend %q", backendName)
		}
		projectDirectory, stateDirectory, databasePath, err := resolveRuntimePaths(options)
		if err != nil {
			return stateFailureError(err)
		}
		memoryBytes, err := compiler.ParseMemory(maxMemory)
		if err != nil {
			return usageError("invalid --max-memory value %q: %v", maxMemory, err)
		}
		stateStore, err := store.Open(command.Context(), databasePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		taskScheduler, err := scheduler.New(plan, stateStore, scheduler.Options{ProjectDirectory: projectDirectory, StateDirectory: stateDirectory, ConfigPath: options.configPath, WorkflowPath: options.workflowPath, Backend: selectedBackend, MaxParallel: effectiveWorkers, MaxCores: effectiveMaxCores, MaxMemoryBytes: memoryBytes, Force: force, Version: buildInfo.Version, RunID: runID})
		if err != nil {
			return backendFailureError(err)
		}
		actualRunID, runErr := taskScheduler.Run(command.Context())
		for _, logErr := range taskScheduler.ControllerLogErrors() {
			fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", logErr)
		}
		fmt.Fprintf(
			command.OutOrStdout(),
			"run_id: %s\nstate: %s\ncontroller_log: %s\n",
			actualRunID,
			databasePath,
			taskScheduler.ControllerLogPath(),
		)
		return taskFailureError(runErr)
	}}
	addPlanFlags(command, &options)
	command.Flags().StringVar(&backendName, "backend", "local", "Execution backend (local/slurm)")
	command.Flags().StringVar(&slurmPartition, "partition", os.Getenv("CRAFTMAKE_SLURM_PARTITION"), "Override the Slurm partition (or set CRAFTMAKE_SLURM_PARTITION)")
	command.Flags().IntVar(&workers, "workers", 0, "Maximum active submissions; zero uses --max-parallel")
	command.Flags().IntVar(&maxParallel, "max-parallel", runtime.NumCPU(), "Maximum active submissions when --workers is zero")
	command.Flags().IntVar(&maxCores, "max-cores", 0, "Scheduler CPU admission limit; Local defaults to machine CPUs, Slurm defaults to unlimited")
	command.Flags().StringVar(&maxMemory, "max-memory", "0", "Scheduler memory admission limit, 0 means unlimited")
	command.Flags().IntVar(&slurmSubmitAttempts, "slurm-submit-attempts", 8, "Maximum attempts for transient sbatch submission failures")
	command.Flags().DurationVar(&slurmSubmitBackoff, "slurm-submit-backoff", time.Second, "Initial delay after a transient sbatch failure")
	command.Flags().DurationVar(&slurmSubmitMaximumBackoff, "slurm-submit-max-backoff", 30*time.Second, "Maximum delay between transient sbatch retries")
	command.Flags().DurationVar(&slurmPendingTimeout, "slurm-pending-timeout", 0, "Cancel a Slurm job after this continuous pending duration, 0 disables")
	command.Flags().BoolVar(&force, "force", false, "Ignore fingerprint cache")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Compile and display the plan without executing")
	command.Flags().StringVar(&runID, "run-id", "", "Optional run identifier")
	return command
}

func newStatusCommand() *cobra.Command {
	var statePath string
	var runID string
	var verbose bool
	command := &cobra.Command{Use: "status", Short: "Show workflow run status", RunE: func(command *cobra.Command, arguments []string) error {
		stateStore, err := store.Open(command.Context(), statePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		if runID == "" || runID == "latest" {
			runID, err = stateStore.LatestRunID(command.Context())
			if err != nil {
				return stateFailureError(err)
			}
		}
		run, counts, err := stateStore.RunSummary(command.Context(), runID)
		if err != nil {
			return stateFailureError(err)
		}
		fmt.Fprintf(command.OutOrStdout(), "run: %s\nworkflow: %s\nphase: %s\nbackend: %s\nstatus: %s\n", run.ID, run.Workflow, run.Phase, run.Backend, run.Status)
		statuses := make([]string, 0, len(counts))
		for status := range counts {
			statuses = append(statuses, status)
		}
		sort.Strings(statuses)
		for _, status := range statuses {
			fmt.Fprintf(command.OutOrStdout(), "%s: %d\n", status, counts[status])
		}
		if verbose {
			rows, queryErr := stateStore.QueryRows(command.Context(), `
				SELECT task_id, status, COALESCE(cache_decision, ''), COALESCE(cache_reason_code, ''), COALESCE(cache_reason_detail, '')
				FROM task_instances
				WHERE run_id = ?
				ORDER BY task_id
			`, runID)
			if queryErr != nil {
				return stateFailureError(queryErr)
			}
			defer rows.Close()
			fmt.Fprintln(command.OutOrStdout(), "cache_decisions:")
			for rows.Next() {
				var taskID string
				var taskStatus string
				var cacheDecision string
				var reasonCode string
				var reasonDetail string
				if err := rows.Scan(&taskID, &taskStatus, &cacheDecision, &reasonCode, &reasonDetail); err != nil {
					return stateFailureError(err)
				}
				fmt.Fprintf(
					command.OutOrStdout(),
					"%s\tstatus=%s\tdecision=%s\treason=%s\tdetail=%s\n",
					taskID,
					taskStatus,
					cacheDecision,
					reasonCode,
					reasonDetail,
				)
			}
			if err := rows.Err(); err != nil {
				return stateFailureError(err)
			}
		}
		return nil
	}}
	command.Flags().StringVar(&statePath, "state", "workflow/.craftmake/state.sqlite", "State database path")
	command.Flags().StringVar(&runID, "run", "latest", "Run identifier")
	command.Flags().BoolVar(&verbose, "verbose", false, "Show per-task cache decisions and reasons")
	return command
}

func newCancelCommand() *cobra.Command {
	var statePath string
	var runID string
	command := &cobra.Command{Use: "cancel", Short: "Cancel a running workflow", RunE: func(command *cobra.Command, arguments []string) error {
		stateStore, err := store.Open(command.Context(), statePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		if runID == "" || runID == "latest" {
			runID, err = stateStore.LatestRunID(command.Context())
			if err != nil {
				return stateFailureError(err)
			}
		}
		run, _, err := stateStore.RunSummary(command.Context(), runID)
		if err != nil {
			return stateFailureError(err)
		}
		if run.Status != "running" {
			return usageError("run %s is not running (status: %s)", runID, run.Status)
		}
		selectedBackend, err := backendForName(run.Backend)
		if err != nil {
			return err
		}
		submissions, err := stateStore.RunningSubmissions(command.Context(), runID)
		if err != nil {
			return stateFailureError(err)
		}
		structuredLogger, controllerLogPath := openControllerLogger(command, stateStore, statePath, runID)
		structuredLogger.Log(command.Context(), controllerlog.Event{
			Name:    "run.cancellation_requested",
			RunID:   runID,
			Backend: run.Backend,
			Status:  "running",
			Details: map[string]any{"submission_count": len(submissions)},
		})
		finishedAt := time.Now().UTC()
		if err := stateStore.CancelRun(command.Context(), runID, finishedAt); err != nil {
			structuredLogger.Log(command.Context(), controllerlog.Event{
				Timestamp:            finishedAt,
				Level:                "error",
				Name:                 "run.finished",
				RunID:                runID,
				Backend:              run.Backend,
				Status:               "failed",
				DurationMilliseconds: finishedAt.Sub(run.StartedAt).Milliseconds(),
				Error:                err.Error(),
			})
			_ = structuredLogger.Close()
			reportControllerLogErrors(command, structuredLogger)
			return stateFailureError(err)
		}
		var cancellationErrors []string
		for _, submission := range submissions {
			metadata := map[string]any{}
			if len(submission.RawMetadata) > 0 {
				if err := json.Unmarshal(submission.RawMetadata, &metadata); err != nil {
					cancellationErrors = append(cancellationErrors, fmt.Sprintf("submission %s metadata: %v", submission.ID, err))
					structuredLogger.Log(command.Context(), controllerlog.Event{
						Level:        "error",
						Name:         "submission.cancellation_finished",
						RunID:        runID,
						SubmissionID: submission.ID,
						Backend:      run.Backend,
						BackendJobID: submission.BackendJobID,
						Status:       "failed",
						Error:        err.Error(),
					})
					continue
				}
			}
			cancellationErr := selectedBackend.CancelSubmission(command.Context(), submission.BackendJobID, metadata)
			submissionStatus := "cancelled"
			if cancellationErr != nil {
				submissionStatus = "failed"
				cancellationErrors = append(cancellationErrors, fmt.Sprintf("submission %s: %v", submission.ID, cancellationErr))
			}
			structuredLogger.Log(command.Context(), controllerlog.Event{
				Level:        controllerEventLevelForStatus(submissionStatus),
				Name:         "submission.cancellation_finished",
				RunID:        runID,
				SubmissionID: submission.ID,
				Backend:      run.Backend,
				BackendJobID: submission.BackendJobID,
				Status:       submissionStatus,
				Error:        controllerErrorMessage(cancellationErr),
			})
		}
		var cancellationErr error
		if len(cancellationErrors) > 0 {
			cancellationErr = fmt.Errorf("cancel run %s: %s", runID, strings.Join(cancellationErrors, "; "))
		}
		controllerFinishedAt := time.Now().UTC()
		structuredLogger.Log(command.Context(), controllerlog.Event{
			Timestamp:            controllerFinishedAt,
			Level:                "warn",
			Name:                 "run.finished",
			RunID:                runID,
			Backend:              run.Backend,
			Status:               "cancelled",
			DurationMilliseconds: controllerFinishedAt.Sub(run.StartedAt).Milliseconds(),
			Error:                controllerErrorMessage(cancellationErr),
		})
		_ = structuredLogger.Close()
		reportControllerLogErrors(command, structuredLogger)
		if cancellationErr != nil {
			return backendFailureError(cancellationErr)
		}
		fmt.Fprintf(command.OutOrStdout(), "cancelled: %s\ncontroller_log: %s\n", runID, controllerLogPath)
		return nil
	}}
	command.Flags().StringVar(&statePath, "state", "workflow/.craftmake/state.sqlite", "State database path")
	command.Flags().StringVar(&runID, "run", "latest", "Run identifier")
	return command
}

func openControllerLogger(
	command *cobra.Command,
	stateStore *store.Store,
	statePath string,
	runID string,
) (*controllerlog.Logger, string) {
	controllerLogPath := controllerlog.DefaultPath(filepath.Dir(statePath), runID)
	structuredLogger, err := controllerlog.Open(controllerLogPath, stateStore)
	if err != nil {
		fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", err)
		structuredLogger = controllerlog.New(nil, stateStore)
	}
	return structuredLogger, controllerLogPath
}

func reportControllerLogErrors(command *cobra.Command, structuredLogger *controllerlog.Logger) {
	for _, logErr := range structuredLogger.Errors() {
		fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", logErr)
	}
}

func controllerEventLevelForStatus(status string) string {
	switch status {
	case "failed":
		return "error"
	case "cancelled", "interrupted":
		return "warn"
	default:
		return "info"
	}
}

func controllerErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func backendForName(backendName string) (backend.Backend, error) {
	return backendForNameWithPartition(backendName, "")
}

func backendForNameWithPartition(backendName, slurmPartition string) (backend.Backend, error) {
	switch backendName {
	case "local":
		return local.New(), nil
	case "slurm":
		return slurm.NewWithPartition(slurmPartition), nil
	default:
		return nil, backendFailureError(fmt.Errorf("unsupported persisted backend %q", backendName))
	}
}

func resolveEffectiveWorkers(workers, maxParallel int) (int, error) {
	if workers < 0 {
		return 0, usageError("--workers must be zero or positive")
	}
	if maxParallel <= 0 {
		return 0, usageError("--max-parallel must be positive")
	}
	if workers > 0 {
		return workers, nil
	}
	return maxParallel, nil
}

func effectiveSchedulerMaxCores(backendName string, maxCores int, maxCoresFlagChanged bool) int {
	if backendName == "local" && !maxCoresFlagChanged {
		return runtime.NumCPU()
	}
	return maxCores
}

func configuredSlurmBackend(
	partition string,
	submitAttempts int,
	submitBackoff time.Duration,
	submitMaximumBackoff time.Duration,
	pendingTimeout time.Duration,
) (*slurm.Backend, error) {
	if submitAttempts <= 0 {
		return nil, usageError("--slurm-submit-attempts must be positive")
	}
	if submitBackoff <= 0 {
		return nil, usageError("--slurm-submit-backoff must be positive")
	}
	if submitMaximumBackoff <= 0 {
		return nil, usageError("--slurm-submit-max-backoff must be positive")
	}
	if submitMaximumBackoff < submitBackoff {
		return nil, usageError("--slurm-submit-max-backoff must be at least --slurm-submit-backoff")
	}
	if pendingTimeout < 0 {
		return nil, usageError("--slurm-pending-timeout must be zero or positive")
	}
	selectedBackend := slurm.NewWithPartition(partition)
	selectedBackend.SubmitMaxAttempts = submitAttempts
	selectedBackend.SubmitInitialBackoff = submitBackoff
	selectedBackend.SubmitMaximumBackoff = submitMaximumBackoff
	selectedBackend.PendingTimeout = pendingTimeout
	return selectedBackend, nil
}

func newReportCommand() *cobra.Command {
	var statePath string
	var runID string
	var outputDirectory string
	var format string
	var refreshMetrics bool
	command := &cobra.Command{Use: "report", Short: "Export run metrics and timings", RunE: func(command *cobra.Command, arguments []string) error {
		if format != "csv" {
			return usageError("unsupported report format %q", format)
		}
		stateStore, err := store.Open(command.Context(), statePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		if runID == "" || runID == "latest" {
			runID, err = stateStore.LatestRunID(command.Context())
			if err != nil {
				return stateFailureError(err)
			}
		}
		if refreshMetrics {
			refreshSummary, refreshErr := report.RefreshMetrics(command.Context(), stateStore, runID, slurm.New())
			if refreshErr != nil {
				return stateFailureError(refreshErr)
			}
			for _, refreshFailure := range refreshSummary.Failures {
				fmt.Fprintf(command.ErrOrStderr(), "warning: metrics refresh for attempt %s failed: %v\n", refreshFailure.AttemptID, refreshFailure.Err)
			}
			fmt.Fprintf(
				command.OutOrStdout(),
				"metrics_refresh_candidates: %d\nmetrics_refreshed: %d\nmetrics_unavailable: %d\n",
				refreshSummary.Candidates,
				refreshSummary.Refreshed,
				len(refreshSummary.Failures),
			)
		}
		if outputDirectory == "" {
			outputDirectory = filepath.Join(filepath.Dir(statePath), "runs", runID, "reports")
		}
		if err := report.ExportCSV(command.Context(), stateStore, runID, outputDirectory); err != nil {
			return stateFailureError(err)
		}
		fmt.Fprintln(command.OutOrStdout(), outputDirectory)
		return nil
	}}
	command.Flags().StringVar(&statePath, "state", "workflow/.craftmake/state.sqlite", "State database path")
	command.Flags().StringVar(&runID, "run", "latest", "Run identifier")
	command.Flags().StringVar(&outputDirectory, "output", "", "Report output directory")
	command.Flags().StringVar(&format, "format", "csv", "Report format")
	command.Flags().BoolVar(&refreshMetrics, "refresh-metrics", false, "Retry unavailable Slurm accounting metrics before exporting")
	return command
}

func newLogsCommand() *cobra.Command {
	var statePath string
	var runID string
	var failedOnly bool
	command := &cobra.Command{Use: "logs", Short: "List task log paths", RunE: func(command *cobra.Command, arguments []string) error {
		stateStore, err := store.Open(command.Context(), statePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		if runID == "" || runID == "latest" {
			runID, err = stateStore.LatestRunID(command.Context())
			if err != nil {
				return stateFailureError(err)
			}
		}
		query := `SELECT task_id, status, result_path FROM task_attempts WHERE run_id=?`
		fmt.Fprintf(
			command.OutOrStdout(),
			"controller\t%s\t%s\n",
			runID,
			controllerlog.DefaultPath(filepath.Dir(statePath), runID),
		)
		if failedOnly {
			query += ` AND status='failed'`
		}
		query += ` ORDER BY task_id, attempt_number`
		rows, err := stateStore.QueryRows(command.Context(), query, runID)
		if err != nil {
			return stateFailureError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var taskID, status, resultPath string
			if err := rows.Scan(&taskID, &status, &resultPath); err != nil {
				return stateFailureError(err)
			}
			fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\n", status, taskID, filepath.Dir(resultPath))
		}
		return stateFailureError(rows.Err())
	}}
	command.Flags().StringVar(&statePath, "state", "workflow/.craftmake/state.sqlite", "State database path")
	command.Flags().StringVar(&runID, "run", "latest", "Run identifier")
	command.Flags().BoolVar(&failedOnly, "failed", false, "Show only failed task logs")
	return command
}

func newDoctorCommand() *cobra.Command {
	var backendName string
	command := &cobra.Command{Use: "doctor", Short: "Check runtime dependencies", RunE: func(command *cobra.Command, arguments []string) error {
		if backendName != "local" && backendName != "slurm" {
			return usageError("unsupported backend %q", backendName)
		}
		if backendName == "local" {
			fmt.Fprintf(command.OutOrStdout(), "local: ok\ngnu_time: %t\nenva_or_conda: optional\n", fileExists("/usr/bin/time"))
			return nil
		}
		missing := []string{}
		for _, executable := range []string{"sinfo", "sbatch", "squeue", "sacct", "scancel", "srun"} {
			if _, err := execLookPath(executable); err != nil {
				missing = append(missing, executable)
			}
		}
		if len(missing) > 0 {
			return backendFailureError(fmt.Errorf("missing Slurm commands: %s", strings.Join(missing, ", ")))
		}
		fmt.Fprintln(command.OutOrStdout(), "slurm: ok")
		return nil
	}}
	command.Flags().StringVar(&backendName, "backend", "local", "Backend to check")
	return command
}

func newResumeCommand(buildInfo BuildInfo) *cobra.Command {
	var statePath string
	var runID string
	var slurmPartition string
	var workers int
	var maxParallel int
	var maxCores int
	var maxMemory string
	var slurmSubmitAttempts int
	var slurmSubmitBackoff time.Duration
	var slurmSubmitMaximumBackoff time.Duration
	var slurmPendingTimeout time.Duration
	command := &cobra.Command{Use: "resume", Short: "Recover and resume a prior run using its workflow, config, and backend", RunE: func(command *cobra.Command, arguments []string) error {
		stateStore, err := store.Open(command.Context(), statePath)
		if err != nil {
			return stateFailureError(err)
		}
		defer stateStore.Close()
		if runID == "" || runID == "latest" {
			runID, err = stateStore.LatestRunID(command.Context())
			if err != nil {
				return stateFailureError(err)
			}
		}
		run, _, err := stateStore.RunSummary(command.Context(), runID)
		if err != nil {
			return stateFailureError(err)
		}
		selectedBackend, err := backendForNameWithPartition(run.Backend, slurmPartition)
		if err != nil {
			return err
		}
		effectiveWorkers, workerErr := resolveEffectiveWorkers(workers, maxParallel)
		if workerErr != nil {
			return workerErr
		}
		effectiveMaxCores := effectiveSchedulerMaxCores(run.Backend, maxCores, command.Flags().Changed("max-cores"))
		if run.Backend == "slurm" {
			selectedBackend, err = configuredSlurmBackend(
				slurmPartition,
				slurmSubmitAttempts,
				slurmSubmitBackoff,
				slurmSubmitMaximumBackoff,
				slurmPendingTimeout,
			)
			if err != nil {
				return err
			}
		}
		projectDirectory := filepath.Dir(run.ConfigPath)
		if run.Status == "running" {
			recoveryLogPath := controllerlog.DefaultPath(filepath.Dir(statePath), runID)
			recoveryLogger, recoveryLogOpenErr := controllerlog.Open(recoveryLogPath, stateStore)
			if recoveryLogOpenErr != nil {
				fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", recoveryLogOpenErr)
				recoveryLogger = controllerlog.New(nil, stateStore)
			}
			recoverySummary, recoveryErr := scheduler.ReconcileRun(command.Context(), stateStore, selectedBackend, runID, projectDirectory, recoveryLogger)
			_ = recoveryLogger.Close()
			for _, logErr := range recoveryLogger.Errors() {
				fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", logErr)
			}
			if recoveryErr != nil {
				return backendFailureError(fmt.Errorf("recover source run %s: %w", runID, recoveryErr))
			}
			fmt.Fprintf(
				command.OutOrStdout(),
				"recovered_run: %s\nrecovered_status: %s\nrecovered_succeeded: %d\nrecovered_failed: %d\nrecovered_cancelled: %d\nrecovered_interrupted: %d\nrecovered_controller_log: %s\n",
				runID,
				recoverySummary.FinalStatus,
				recoverySummary.Succeeded,
				recoverySummary.Failed,
				recoverySummary.Cancelled,
				recoverySummary.Interrupted,
				recoveryLogPath,
			)
		}

		options := commonOptions{
			workflowPath: run.WorkflowPath,
			configPath:   run.ConfigPath,
			projectDir:   projectDirectory,
			stateDir:     filepath.Dir(statePath),
		}
		plan, err := loadPlan(&options)
		if err != nil {
			return err
		}
		memoryBytes, err := compiler.ParseMemory(maxMemory)
		if err != nil {
			return usageError("invalid --max-memory value %q: %v", maxMemory, err)
		}
		taskScheduler, err := scheduler.New(plan, stateStore, scheduler.Options{
			ProjectDirectory: projectDirectory,
			StateDirectory:   options.stateDir,
			ConfigPath:       options.configPath,
			WorkflowPath:     options.workflowPath,
			Backend:          selectedBackend,
			MaxParallel:      effectiveWorkers,
			MaxCores:         effectiveMaxCores,
			MaxMemoryBytes:   memoryBytes,
			Version:          buildInfo.Version,
			ResumedFromRunID: runID,
		})
		if err != nil {
			return backendFailureError(err)
		}
		newRunID, runErr := taskScheduler.Run(command.Context())
		for _, logErr := range taskScheduler.ControllerLogErrors() {
			fmt.Fprintf(command.ErrOrStderr(), "warning: controller log: %v\n", logErr)
		}
		fmt.Fprintf(
			command.OutOrStdout(),
			"resumed_from: %s\nrun_id: %s\nbackend: %s\ncontroller_log: %s\n",
			runID,
			newRunID,
			run.Backend,
			taskScheduler.ControllerLogPath(),
		)
		return taskFailureError(runErr)
	}}
	command.Flags().StringVar(&statePath, "state", "workflow/.craftmake/state.sqlite", "State database path")
	command.Flags().StringVar(&runID, "run", "latest", "Run identifier")
	command.Flags().StringVar(&slurmPartition, "partition", os.Getenv("CRAFTMAKE_SLURM_PARTITION"), "Override the Slurm partition (or set CRAFTMAKE_SLURM_PARTITION)")
	command.Flags().IntVar(&workers, "workers", 0, "Maximum active submissions; zero uses --max-parallel")
	command.Flags().IntVar(&maxParallel, "max-parallel", runtime.NumCPU(), "Maximum active submissions when --workers is zero")
	command.Flags().IntVar(&maxCores, "max-cores", 0, "Scheduler CPU admission limit; Local defaults to machine CPUs, Slurm defaults to unlimited")
	command.Flags().StringVar(&maxMemory, "max-memory", "0", "Scheduler memory admission limit, 0 means unlimited")
	command.Flags().IntVar(&slurmSubmitAttempts, "slurm-submit-attempts", 8, "Maximum attempts for transient sbatch submission failures")
	command.Flags().DurationVar(&slurmSubmitBackoff, "slurm-submit-backoff", time.Second, "Initial delay after a transient sbatch failure")
	command.Flags().DurationVar(&slurmSubmitMaximumBackoff, "slurm-submit-max-backoff", 30*time.Second, "Maximum delay between transient sbatch retries")
	command.Flags().DurationVar(&slurmPendingTimeout, "slurm-pending-timeout", 0, "Cancel a Slurm job after this continuous pending duration, 0 disables")
	return command
}

func newTaskRunnerCommand() *cobra.Command {
	var manifestPath string
	command := &cobra.Command{Use: "__task-runner", Hidden: true, RunE: func(command *cobra.Command, arguments []string) error {
		manifest, err := runtimeexecutor.LoadManifest(manifestPath)
		if err != nil {
			return internalFailureError(err)
		}
		_, err = runtimeexecutor.Run(command.Context(), manifest)
		return taskFailureError(err)
	}}
	command.Flags().StringVar(&manifestPath, "manifest", "", "Task manifest path")
	_ = command.MarkFlagRequired("manifest")
	return command
}

func addPlanFlags(command *cobra.Command, options *commonOptions) {
	command.Flags().StringVarP(&options.workflowPath, "workflow", "w", "", "Workflow YAML path; overrides automatic catalog routing")
	command.Flags().StringVarP(&options.configPath, "config", "c", "", "xdxtools config YAML path")
	command.Flags().StringVar(&options.phase, "phase", "", "Workflow phase to resolve automatically, for example step2-check")
	command.Flags().StringVar(&options.catalogDir, "catalog", "", "Workflow catalog root; defaults to CRAFTMAKE_WORKFLOW_CATALOG or installed workflows")
	command.Flags().StringVar(&options.projectDir, "project-dir", "", "Project working directory")
	command.Flags().StringVar(&options.stateDir, "state-dir", "", "Craftmake state directory")
	_ = command.MarkFlagRequired("config")
}

func loadPlan(options *commonOptions) (*compiler.Plan, error) {
	if options.configPath == "" {
		return nil, usageError("--config is required")
	}
	absoluteConfigPath, err := filepath.Abs(options.configPath)
	if err != nil {
		return nil, configurationError(fmt.Errorf("resolve config path: %w", err))
	}
	options.configPath = absoluteConfigPath
	context, err := xdxtools.Load(options.configPath)
	if err != nil {
		return nil, configurationError(err)
	}

	if options.workflowPath == "" {
		if options.phase == "" {
			return nil, usageError("--phase is required when --workflow is not provided")
		}
		workflowPath, resolveErr := resolveCatalogWorkflow(options.catalogDir, context.Workflow.WorkflowName, options.phase)
		if resolveErr != nil {
			return nil, configurationError(resolveErr)
		}
		options.workflowPath = workflowPath
	} else {
		absoluteWorkflowPath, resolveErr := filepath.Abs(options.workflowPath)
		if resolveErr != nil {
			return nil, configurationError(fmt.Errorf("resolve workflow path: %w", resolveErr))
		}
		options.workflowPath = absoluteWorkflowPath
	}

	workflow, err := spec.Load(options.workflowPath)
	if err != nil {
		return nil, configurationError(err)
	}
	if !strings.EqualFold(workflow.On.Xdxtools.Workflow, context.Workflow.WorkflowName) {
		return nil, configurationError(fmt.Errorf("workflow %q targets %s, but config resolves to %s", options.workflowPath, workflow.On.Xdxtools.Workflow, context.Workflow.WorkflowName))
	}
	if options.phase != "" && workflow.On.Xdxtools.Phase != options.phase {
		return nil, configurationError(fmt.Errorf("workflow %q declares phase %q, not requested phase %q", options.workflowPath, workflow.On.Xdxtools.Phase, options.phase))
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		return nil, compilationError(err)
	}
	return plan, nil
}

func resolveCatalogWorkflow(configuredCatalog, workflowName, phase string) (string, error) {
	if !isCatalogComponent(workflowName) {
		return "", configurationError(fmt.Errorf("config resolved to invalid workflow name %q", workflowName))
	}
	if !isCatalogComponent(phase) {
		return "", usageError("invalid workflow phase %q", phase)
	}

	catalogCandidates := workflowCatalogCandidates(configuredCatalog)
	searchedPaths := make([]string, 0, len(catalogCandidates))
	for _, catalogDirectory := range catalogCandidates {
		workflowPath := filepath.Join(catalogDirectory, workflowName, phase+".yaml")
		searchedPaths = append(searchedPaths, workflowPath)
		fileInfo, statErr := os.Stat(workflowPath)
		if statErr == nil && !fileInfo.IsDir() {
			absoluteWorkflowPath, absoluteErr := filepath.Abs(workflowPath)
			if absoluteErr != nil {
				return "", fmt.Errorf("resolve discovered workflow path: %w", absoluteErr)
			}
			return absoluteWorkflowPath, nil
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect workflow %q: %w", workflowPath, statErr)
		}
	}
	return "", fmt.Errorf("workflow catalog entry %s/%s.yaml was not found; searched: %s", workflowName, phase, strings.Join(searchedPaths, ", "))
}

func workflowCatalogCandidates(configuredCatalog string) []string {
	rawCandidates := []string{}
	if configuredCatalog != "" {
		rawCandidates = append(rawCandidates, configuredCatalog)
	} else if environmentCatalog := os.Getenv("CRAFTMAKE_WORKFLOW_CATALOG"); environmentCatalog != "" {
		rawCandidates = append(rawCandidates, environmentCatalog)
	} else {
		if executablePath, err := os.Executable(); err == nil {
			executableDirectory := filepath.Dir(executablePath)
			rawCandidates = append(rawCandidates,
				filepath.Join(executableDirectory, "workflows"),
				filepath.Join(executableDirectory, "..", "share", "craftmake", "workflows"),
			)
		}
		if workingDirectory, err := os.Getwd(); err == nil {
			rawCandidates = append(rawCandidates, filepath.Join(workingDirectory, "workflows"))
		}
		if _, sourcePath, _, ok := runtime.Caller(0); ok {
			rawCandidates = append(rawCandidates, filepath.Join(filepath.Dir(sourcePath), "..", "..", "workflows"))
		}
	}

	uniqueCandidates := make([]string, 0, len(rawCandidates))
	seenCandidates := make(map[string]bool)
	for _, rawCandidate := range rawCandidates {
		absoluteCandidate, err := filepath.Abs(rawCandidate)
		if err != nil {
			continue
		}
		cleanCandidate := filepath.Clean(absoluteCandidate)
		if !seenCandidates[cleanCandidate] {
			seenCandidates[cleanCandidate] = true
			uniqueCandidates = append(uniqueCandidates, cleanCandidate)
		}
	}
	return uniqueCandidates
}

func isCatalogComponent(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\\`)
}

func resolveRuntimePaths(options commonOptions) (string, string, string, error) {
	projectDirectory := options.projectDir
	if projectDirectory == "" {
		projectDirectory = filepath.Dir(options.configPath)
	}
	absoluteProject, err := filepath.Abs(projectDirectory)
	if err != nil {
		return "", "", "", err
	}
	stateDirectory := options.stateDir
	if stateDirectory == "" {
		stateDirectory = filepath.Join(absoluteProject, "workflow", ".craftmake")
	}
	absoluteState, err := filepath.Abs(stateDirectory)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(absoluteState, 0o755); err != nil {
		return "", "", "", err
	}
	return absoluteProject, absoluteState, filepath.Join(absoluteState, "state.sqlite"), nil
}

func printPlan(command *cobra.Command, plan *compiler.Plan) {
	fmt.Fprintf(command.OutOrStdout(), "Workflow: %s\nPhase: %s\nTasks: %d\nSubmissions: %d\n\n", plan.Workflow, plan.Phase, len(plan.Tasks), len(plan.Submissions))
	for orderIndex, taskID := range plan.Order {
		task := plan.TaskByID[taskID]
		fmt.Fprintf(command.OutOrStdout(), "%03d  %-8s  cores=%d  memory=%d  %s\n", orderIndex+1, task.Scope, task.Resources.Cores, task.Resources.MemoryByte, task.ID)
		if len(task.Dependencies) > 0 {
			fmt.Fprintf(command.OutOrStdout(), "     needs: %s\n", strings.Join(task.Dependencies, ", "))
		}
	}
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
