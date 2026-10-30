package service

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
	"time"
)

func TestWorkspacePathTraversal(t *testing.T) {
	for _, p := range []string{"", ".", "../other", "/tmp/file", "outputs/../../x", "outputs\\x", "a\x00b"} {
		if validWorkspacePath(p) {
			t.Fatalf("accepted unsafe path %q", p)
		}
	}
	for _, p := range []string{"outputs", "results/model.bin"} {
		if !validWorkspacePath(p) {
			t.Fatalf("rejected %q", p)
		}
	}
}
func TestWorkspaceTransferRejectsWrongToken(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}}
	m.ExpectQuery(`SELECT user_id,workspace,input_token_hash FROM colab_jobs WHERE id=\$1 AND status='running'`).WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"user_id", "workspace", "input_token_hash"}).AddRow(1, `{"input_node_id":2}`, strings.Repeat("0", 64)))
	f, _, e := s.WorkspaceInput(context.Background(), "job", strings.Repeat("a", 43))
	if e == nil || f != nil {
		t.Fatal("wrong token accepted")
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestWorkspaceTransferRejectsTerminalJob(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}}
	m.ExpectQuery(`SELECT user_id,workspace,input_token_hash FROM colab_jobs`).WithArgs("job").WillReturnError(sql.ErrNoRows)
	if _, _, e := s.WorkspaceInput(context.Background(), "job", strings.Repeat("a", 43)); e == nil {
		t.Fatal("terminal job transfer accepted")
	}
}
func TestComputeUserStatusIsScoped(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}, drive: &GoogleDriveService{}}
	m.ExpectQuery(`SELECT id,email,enabled,subscription,runtime_specs,last_error,checked_at,consumption,consumption_checked_at,consumption_error FROM colab_accounts`).WillReturnRows(sqlmock.NewRows([]string{"id", "email", "enabled", "subscription", "runtime_specs", "last_error", "checked_at", "consumption", "consumption_checked_at", "consumption_error"}))
	m.ExpectQuery(`SELECT account_id, COUNT`).WillReturnRows(sqlmock.NewRows([]string{"account_id", "count", "total", "gpu", "a100"}))
	m.ExpectQuery(`SELECT COUNT\(\*\).*FROM colab_jobs WHERE user_id=\$1 OR \$2`).WithArgs(int64(42), false).WillReturnRows(sqlmock.NewRows([]string{"count", "succeeded", "total", "gpu", "a100", "queued", "running", "failed", "charged", "reserved"}).AddRow(2, 1, 20, 10, 0, 1, 0, 0, "0.042", "0.3"))
	m.ExpectQuery(`SELECT balance FROM users WHERE id=\$1`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.5))
	m.ExpectQuery(`SELECT enabled,version,rates FROM compute_pricing`).WillReturnRows(sqlmock.NewRows([]string{"enabled", "version", "rates"}).AddRow(false, 1, `{}`))
	out, e := s.Status(context.Background(), 42, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := out["accounts"]; ok {
		t.Fatal("accounts leaked")
	}
	summary := out["usage_summary"].(map[string]any)
	if summary["queued_jobs"] != 1 || fmt.Sprint(summary["charged_cost"]) != "0.042" || fmt.Sprint(summary["reserved_cost"]) != "0.3" {
		t.Fatalf("wrong scoped aggregate: %v", summary)
	}
	if out["compute_units"] != nil {
		t.Fatal("Google CU leaked to user")
	}
	if out["platform_credits"] != 12.5 {
		t.Fatal("wrong credits")
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestComputeLegacyJobWithNullWorkspace(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}}
	id := "6530b4e8-372e-44c0-8f75-b652f9c36d75"
	columns := strings.Split(jobColumns, ",")
	m.ExpectQuery(`SELECT .* FROM colab_jobs`).WithArgs(id, int64(1), false).WillReturnRows(sqlmock.NewRows(columns).AddRow(id, 1, 2, "legacy", "print(1)", "CPU", "succeeded", 180, "private-runtime", "private-operation", true, false, "ok", "", "/out", time.Now(), time.Now(), nil, nil, nil, "0", "0", "0", 0, "free"))
	j, e := s.Job(context.Background(), 1, id, false)
	if e != nil {
		t.Fatal(e)
	}
	if j.AllocatedAt != nil || j.DurationSeconds != 0 || len(j.Workspace) != 0 {
		t.Fatal("invented old runtime or workspace")
	}
	if j.Runtime != "" || j.Operation != "" || j.AccountID != nil {
		t.Fatal("upstream identifiers leaked")
	}
}
