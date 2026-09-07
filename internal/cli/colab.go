package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	colab "github.com/fallingstar10/craftmake/internal/backend/colab"
	"github.com/spf13/cobra"
)

func newColabCommand() *cobra.Command {
	parent := &cobra.Command{Use: "colab", Short: "Configure and inspect Google Colab execution"}
	auth := &cobra.Command{Use: "auth", Short: "Manage Colab session authentication"}
	auth.AddCommand(newColabAuthConfigureCommand(), newColabAuthShowCommand())
	drive := &cobra.Command{Use: "drive", Short: "Manage Google Drive session mounts"}
	drive.AddCommand(newColabDriveMountCommand())
	parent.AddCommand(auth, drive, newColabDoctorCommand())
	return parent
}

func newColabAuthConfigureCommand() *cobra.Command {
	var configPath, sessionID, driveRoot, mountPath, colabCredential, driveCredential, colabTokenEnv, driveTokenEnv string
	command := &cobra.Command{Use: "configure", Short: "Write credentials references for a Colab session", Args: noArguments, RunE: func(command *cobra.Command, _ []string) error {
		path, err := expandUserPath(configPath)
		if err != nil {
			return usageError("invalid --config: %v", err)
		}
		auth := colab.SessionAuth{SessionID: sessionID, DriveRoot: driveRoot, MountPath: mountPath, ColabCredentialFile: colabCredential, DriveCredentialFile: driveCredential, ColabRefreshTokenEnv: colabTokenEnv, DriveRefreshTokenEnv: driveTokenEnv}
		if err := colab.UpsertSessionAuth(path, auth); err != nil {
			return configurationError(err)
		}
		fmt.Fprintf(command.OutOrStdout(), "session: %s\nconfig: %s\ndrive_root: %s\nmount_path: %s\n", sessionID, path, driveRoot, mountPath)
		return nil
	}}
	command.Flags().StringVar(&configPath, "config", "~/.config/craftmake/colab-auth.json", "Authentication config path")
	command.Flags().StringVar(&sessionID, "session", "", "Named Colab session")
	command.Flags().StringVar(&driveRoot, "drive-root", "", "Mounted Drive directory used as durable workspace")
	command.Flags().StringVar(&mountPath, "mount-path", "/content/drive", "Runtime Drive mount point")
	command.Flags().StringVar(&colabCredential, "colab-credential-file", "", "Colab credential file reference")
	command.Flags().StringVar(&driveCredential, "drive-credential-file", "", "Drive credential file reference")
	command.Flags().StringVar(&colabTokenEnv, "colab-refresh-token-env", "CRAFTMAKE_COLAB_REFRESH_TOKEN", "Environment variable containing Colab refresh token")
	command.Flags().StringVar(&driveTokenEnv, "drive-refresh-token-env", "CRAFTMAKE_DRIVE_REFRESH_TOKEN", "Environment variable containing Drive refresh token")
	return command
}

func newColabAuthShowCommand() *cobra.Command {
	var configPath, sessionID string
	command := &cobra.Command{Use: "show", Short: "Show non-secret session authentication configuration", Args: noArguments, RunE: func(command *cobra.Command, _ []string) error {
		path, err := expandUserPath(configPath)
		if err != nil {
			return usageError("invalid --config: %v", err)
		}
		auth, err := colab.LoadSessionAuth(path, sessionID)
		if err != nil {
			return configurationError(err)
		}
		data, _ := json.MarshalIndent(auth, "", "  ")
		fmt.Fprintln(command.OutOrStdout(), string(data))
		return nil
	}}
	command.Flags().StringVar(&configPath, "config", "~/.config/craftmake/colab-auth.json", "Authentication config path")
	command.Flags().StringVar(&sessionID, "session", "", "Named Colab session")
	return command
}

func newColabDriveMountCommand() *cobra.Command {
	var configPath, sessionID string
	command := &cobra.Command{Use: "mount", Short: "Validate and print the Drive mount plan for a session", Args: noArguments, RunE: func(command *cobra.Command, _ []string) error {
		path, err := expandUserPath(configPath)
		if err != nil {
			return usageError("invalid --config: %v", err)
		}
		auth, err := colab.LoadSessionAuth(path, sessionID)
		if err != nil {
			return configurationError(err)
		}
		plan := map[string]any{"session": auth.SessionID, "auth_config": path, "mount_path": auth.MountPath, "drive_root": auth.DriveRoot, "status": "ready-for-backend-mount"}
		data, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Fprintln(command.OutOrStdout(), string(data))
		return nil
	}}
	command.Flags().StringVar(&configPath, "config", "~/.config/craftmake/colab-auth.json", "Authentication config path")
	command.Flags().StringVar(&sessionID, "session", "", "Named Colab session")
	return command
}

func newColabDoctorCommand() *cobra.Command {
	var configPath, sessionID string
	command := &cobra.Command{Use: "doctor", Short: "Check offline Colab session readiness", Args: noArguments, RunE: func(command *cobra.Command, _ []string) error {
		path, err := expandUserPath(configPath)
		if err != nil {
			return usageError("invalid --config: %v", err)
		}
		checks := []map[string]any{}
		if _, statErr := os.Stat(path); statErr != nil {
			checks = append(checks, map[string]any{"name": "auth_config", "ok": false, "detail": statErr.Error()})
		} else {
			checks = append(checks, map[string]any{"name": "auth_config", "ok": true, "detail": path})
		}
		if sessionID == "" {
			checks = append(checks, map[string]any{"name": "session", "ok": false, "detail": "--session is required"})
		} else if auth, loadErr := colab.LoadSessionAuth(path, sessionID); loadErr != nil {
			checks = append(checks, map[string]any{"name": "session", "ok": false, "detail": loadErr.Error()})
		} else {
			checks = append(checks, map[string]any{"name": "session", "ok": true, "detail": auth.SessionID})
			checks = append(checks, map[string]any{"name": "control_plane_credential", "ok": auth.ColabCredentialFile != "" || auth.ColabRefreshTokenEnv != ""})
			checks = append(checks, map[string]any{"name": "drive_credential", "ok": auth.DriveCredentialFile != "" || auth.DriveRefreshTokenEnv != ""})
			checks = append(checks, map[string]any{"name": "mount_config", "ok": auth.MountPath != "" && auth.DriveRoot != ""})
		}
		ready := true
		for _, check := range checks {
			if ok, _ := check["ok"].(bool); !ok {
				ready = false
			}
		}
		data, _ := json.MarshalIndent(map[string]any{"session": sessionID, "auth_config": path, "ready": ready, "mode": "offline-preflight", "checks": checks}, "", "  ")
		fmt.Fprintln(command.OutOrStdout(), string(data))
		if !ready {
			return configurationError(fmt.Errorf("Colab session is not ready"))
		}
		return nil
	}}
	command.Flags().StringVar(&configPath, "config", "~/.config/craftmake/colab-auth.json", "Authentication config path")
	command.Flags().StringVar(&sessionID, "session", "", "Named Colab session")
	return command
}

func expandUserPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if path != "~" && len(path) > 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}
