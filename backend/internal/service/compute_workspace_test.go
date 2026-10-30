package service

import (
	"context"
	"database/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
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
	m.ExpectQuery(`SELECT id,email,enabled,subscription,runtime_specs,last_error,checked_at FROM colab_accounts`).WillReturnRows(sqlmock.NewRows([]string{"id", "email", "enabled", "subscription", "runtime_specs", "last_error", "checked_at"}))
	m.ExpectQuery(`SELECT account_id, COUNT`).WillReturnRows(sqlmock.NewRows([]string{"account_id", "count", "total", "gpu", "a100"}))
	m.ExpectQuery(`SELECT COUNT\(\*\).*FROM colab_jobs WHERE user_id=\$1 OR \$2`).WithArgs(int64(42), false).WillReturnRows(sqlmock.NewRows([]string{"count", "succeeded", "total", "gpu", "a100"}).AddRow(2, 1, 20, 10, 0))
	m.ExpectQuery(`SELECT balance FROM users WHERE id=\$1`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.5))
	out, e := s.Status(context.Background(), 42, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := out["accounts"]; ok {
		t.Fatal("accounts leaked")
	}
	if out["platform_credits"] != 12.5 {
		t.Fatal("wrong credits")
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
