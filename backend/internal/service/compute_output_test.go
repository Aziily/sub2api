package service

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/colab"
	"strings"
	"testing"
)

func TestComputeFailedWorkspaceRetainsLogs(t *testing.T) {
	files := map[string]string{}
	result := &colab.Execution{Stdout: "partial output", Stderr: "setup failed", ExecutionError: []byte(`{"message":"setup failed"}`)}
	err := writeComputeOutput(result, func(name, mime string, data []byte) error {
		files[name] = string(data)
		return nil
	}, func() error {
		if len(files) != 3 {
			t.Fatal("artifacts attempted before logs")
		}
		return errors.New("archive missing")
	})
	if err == nil || !strings.Contains(err.Error(), "Python execution failed") || !strings.Contains(err.Error(), "archive missing") {
		t.Fatalf("lost original execution failure: %v", err)
	}
	if files["stdout.txt"] != "partial output" || files["stderr.txt"] != "setup failed" {
		t.Fatal(files)
	}
}

func TestComputeOutputAttemptsRemainingFilesAfterFailure(t *testing.T) {
	attempts := 0
	err := writeComputeOutput(&colab.Execution{}, func(name, mime string, data []byte) error {
		attempts++
		if name == "execution.json" {
			return errors.New("quota")
		}
		return nil
	}, func() error { attempts++; return nil })
	if err == nil || attempts != 4 {
		t.Fatalf("attempts=%d, error=%v", attempts, err)
	}
}
