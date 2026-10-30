package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestResourceGroupValidation(t *testing.T) {
	for _, kind := range []string{"api", "storage;DROP TABLE users", ""} {
		if _, _, _, err := resourceGroupTables(kind); !errors.Is(err, ErrResourceGroupInput) {
			t.Fatal("accepted unsupported kind")
		}
	}
	for _, in := range []ResourceGroupInput{{Name: " "}, {Name: strings.Repeat("盘", 101)}, {Name: "x", AccountIDs: []int64{1, 1}}, {Name: "x", AccountIDs: []int64{-1}}, {Name: "x", Description: strings.Repeat("a", 1001)}} {
		if !errors.Is(validateResourceGroup(&in), ErrResourceGroupInput) {
			t.Fatal("accepted invalid group")
		}
	}
	in := ResourceGroupInput{Name: " 组 ", AccountIDs: []int64{1, 2}}
	if err := validateResourceGroup(&in); err != nil || in.Name != "组" {
		t.Fatal("valid unicode name rejected")
	}
}
func TestResourceGroupFailedEditDoesNotWrite(t *testing.T) {
	for _, scenario := range []string{"wrong-kind", "stale-version", "missing-member"} {
		t.Run(scenario, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := &ResourceGroupService{db: db}
			m.ExpectBegin()
			q := m.ExpectQuery(`SELECT version FROM resource_groups WHERE id=\$1 AND kind=\$2 FOR UPDATE`).WithArgs(int64(8), "storage")
			expected := ErrResourceGroupInput
			switch scenario {
			case "wrong-kind":
				q.WillReturnError(sql.ErrNoRows)
				expected = ErrResourceGroupMissing
			case "stale-version":
				q.WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
				expected = ErrResourceGroupConflict
			default:
				q.WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))
				m.ExpectQuery(`SELECT id FROM drive_storage_accounts`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			m.ExpectRollback()
			_, err = s.Save(context.Background(), "storage", 8, ResourceGroupInput{Name: "edited", Version: 2, AccountIDs: []int64{999}})
			if !errors.Is(err, expected) {
				t.Fatalf("got %v want %v", err, expected)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestResourceGroupArchiveRetainsMembers(t *testing.T) {
	for _, archived := range []bool{true, false} {
		db, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		s := &ResourceGroupService{db: db}
		m.ExpectBegin()
		m.ExpectQuery(`SELECT version`).WithArgs(int64(8), "compute").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))
		m.ExpectQuery(`SELECT id FROM colab_accounts`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
		m.ExpectQuery(`UPDATE resource_groups`).WithArgs(int64(8), "compute", "team", "", archived).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
		m.ExpectExec(`DELETE FROM resource_group_members`).WithArgs(int64(8), "compute").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectExec(`INSERT INTO resource_group_members\(group_id,kind,compute_account_id\)`).WithArgs(int64(8), "compute", int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectCommit()
		g, err := s.Save(context.Background(), "compute", 8, ResourceGroupInput{Name: "team", Version: 2, Archived: archived, AccountIDs: []int64{5}})
		if err != nil {
			t.Fatal(err)
		}
		if g.Version != 3 || g.Archived != archived || len(g.AccountIDs) != 1 {
			t.Fatalf("unexpected result: %+v", g)
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
func TestResourceGroupDuplicateNameRollsBack(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.ExpectBegin()
	m.ExpectQuery(`INSERT INTO resource_groups`).WillReturnError(&pq.Error{Code: "23505"})
	m.ExpectRollback()
	_, err = (&ResourceGroupService{db: db}).Save(context.Background(), "storage", 0, ResourceGroupInput{Name: "existing"})
	if !errors.Is(err, ErrResourceGroupConflict) {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
