package service

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestComputeJobOwnerIsolation(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}}
	id := "6530b4e8-372e-44c0-8f75-b652f9c36d75"
	m.ExpectQuery(`SELECT .* FROM colab_jobs WHERE id=\$1 AND \(user_id=\$2 OR \$3\)`).WithArgs(id, int64(2), false).WillReturnError(sql.ErrNoRows)
	if _, e = s.Job(context.Background(), 2, id, false); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("cross-user job exposed", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestComputeCancelChecksOwnership(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &ColabService{store: &DriveStore{db: db}}
	id := "6530b4e8-372e-44c0-8f75-b652f9c36d75"
	m.ExpectQuery(`SELECT .* FROM colab_jobs`).WithArgs(id, int64(2), false).WillReturnError(sql.ErrNoRows)
	if e = s.Cancel(context.Background(), 2, id, false); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("cross-user cancellation accepted", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
