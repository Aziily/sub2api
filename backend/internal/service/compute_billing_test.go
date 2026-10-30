package service

import (
	"context"
	"database/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"testing"
)

func TestComputeCostRounding(t *testing.T) {
	for _, tc := range []struct {
		rate    string
		seconds int
		want    string
	}{{"1", 1, "0.00027778"}, {"3.6", 30, "0.03"}, {"0", 3600, "0"}, {"1.5", 0, "0"}, {"0.000001", 1, "0.00000001"}} {
		if got := computeCost(decimal.RequireFromString(tc.rate), tc.seconds); !got.Equal(decimal.RequireFromString(tc.want)) {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}
func TestComputeReservation(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		rates   string
		rows    int64
		err     error
	}{{false, `{}`, 0, nil}, {true, `{}`, 0, ErrComputePrice}, {true, `{"CPU":"3.6"}`, 0, ErrComputeBalance}, {true, `{"CPU":"3.6"}`, 1, nil}} {
		db, m, _ := sqlmock.New()
		m.ExpectBegin()
		tx, _ := db.Begin()
		m.ExpectQuery(`SELECT enabled,rates FROM compute_pricing`).WillReturnRows(sqlmock.NewRows([]string{"enabled", "rates"}).AddRow(tc.enabled, tc.rates))
		if tc.enabled && tc.rates != "{}" {
			m.ExpectExec(`UPDATE users SET balance=balance-`).WithArgs(int64(2), "0.3").WillReturnResult(sqlmock.NewResult(0, tc.rows))
		}
		_, hold, status, e := reserveCompute(context.Background(), tx, 2, "CPU", 300)
		if e != tc.err {
			t.Fatalf("got %v want %v", e, tc.err)
		}
		if tc.enabled && e == nil && (status != "reserved" || hold.String() != "0.3") {
			t.Fatal("bad hold")
		}
		m.ExpectRollback()
		tx.Rollback()
		if e = m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
		db.Close()
	}
}
func TestComputeSettlement(t *testing.T) {
	for _, tc := range []struct {
		seconds      int
		cost, refund string
	}{{0, "0", "0.3"}, {30, "0.03", "0.27"}, {300, "0.3", "0"}} {
		db, m, _ := sqlmock.New()
		s := &ColabService{store: &DriveStore{db: db}}
		m.ExpectBegin()
		m.ExpectQuery(`SELECT user_id,price_per_hour,reserved_cost`).WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"user", "rate", "hold", "seconds"}).AddRow(2, "3.6", "0.3", tc.seconds))
		m.ExpectExec(`UPDATE users SET balance=balance\+`).WithArgs(int64(2), tc.refund).WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectExec(`UPDATE colab_jobs SET charged_cost`).WithArgs("job", tc.cost, tc.seconds).WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectCommit()
		if e := s.settleCompute(context.Background(), "job"); e != nil {
			t.Fatal(e)
		}
		// A repeated settlement cannot return the reservation again.
		m.ExpectBegin()
		m.ExpectQuery(`SELECT user_id,price_per_hour,reserved_cost`).WithArgs("job").WillReturnError(sql.ErrNoRows)
		m.ExpectRollback()
		if e := s.settleCompute(context.Background(), "job"); e != nil {
			t.Fatal(e)
		}
		if e := m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
		db.Close()
	}
}

func TestComputePricingValidation(t *testing.T) {
	s := &ColabService{}
	for _, rates := range []map[string]decimal.Decimal{{"cpu": decimal.Zero}, {"": decimal.Zero}, {"CPU": decimal.NewFromInt(-1)}, {"CPU": decimal.RequireFromString("0.0000001")}, {"H200": decimal.Zero}} {
		if e := s.SetPricing(context.Background(), ComputePricing{Rates: rates}); e == nil {
			t.Fatal("invalid pricing accepted")
		}
	}
}
