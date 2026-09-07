package colab

import (
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/pkg/protocol"
)

func TestBuildNotebookHasBootstrapOneCellPerStepAndFinalizer(t *testing.T) {
	manifest := &protocol.TaskManifest{ProtocolVersion: protocol.Version, RunID: "run-1", TaskID: "task-1", Attempt: 2, WorkDirectory: "/host/work", TempDirectory: "/host/tmp", RuntimeDirectory: "/host/runtime", ResultPath: "/host/runtime/result.json", Steps: []protocol.StepManifest{{Index: 0, Name: "compile", Command: "make", Env: map[string]string{"MODE": "release"}}, {Index: 1, Name: "test", Command: "go test ./..."}}}
	notebook, err := BuildNotebook(manifest, RemoteTaskMapping{WorkDirectory: "/content/craftmake/work", TempDirectory: "/content/craftmake/tmp", RuntimeDirectory: "/content/craftmake/runtime", ResultPath: "/content/craftmake/runtime/result.json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(notebook.Cells) != 4 {
		t.Fatalf("expected bootstrap + 2 steps + finalizer, got %d", len(notebook.Cells))
	}
	if notebook.Cells[1].CellType != "code" || !strings.HasPrefix(notebook.Cells[1].Source, "%%bash\n") {
		t.Fatalf("expected bash cell: %#v", notebook.Cells[1])
	}
	if !strings.Contains(notebook.Cells[1].Source, "CRAFTMAKE_WORK='/content/craftmake/work'") {
		t.Fatalf("missing mapped work directory: %s", notebook.Cells[1].Source)
	}
	if !strings.Contains(notebook.Cells[3].Source, "CRAFTMAKE_TASK_RESULT_BEGIN") {
		t.Fatalf("missing result sentinel")
	}
	if strings.Contains(notebook.Cells[0].Source, "token") {
		t.Fatalf("bootstrap must not contain credentials")
	}
}

func TestDecodeTaskResultUsesSentinelAndProtocolVersion(t *testing.T) {
	output := "noise\nCRAFTMAKE_TASK_RESULT_BEGIN\n{\"protocol_version\":2,\"run_id\":\"run-1\",\"task_id\":\"task-1\",\"attempt\":1,\"status\":\"succeeded\",\"steps\":[]}\nCRAFTMAKE_TASK_RESULT_END\n"
	result, err := DecodeTaskResult(output)
	if err != nil || result.TaskID != "task-1" || result.Status != "succeeded" {
		t.Fatalf("unexpected result %#v, %v", result, err)
	}
	if _, err := DecodeTaskResult(strings.Replace(output, `"protocol_version":2`, `"protocol_version":99`, 1)); err == nil {
		t.Fatal("expected protocol version rejection")
	}
}

func TestPathMapperRejectsOutsideRoot(t *testing.T) {
	mapper := PathMapper{HostRoot: "/repo", RemoteRoot: "/content/work"}
	mapped, err := mapper.Map("/repo/src/main.go")
	if err != nil || mapped != "/content/work/src/main.go" {
		t.Fatalf("unexpected mapped path %q, %v", mapped, err)
	}
	if _, err := mapper.Map("/other/file"); err == nil {
		t.Fatal("expected outside root rejection")
	}
}
