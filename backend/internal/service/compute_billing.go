package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/colab"
	"github.com/shopspring/decimal"
)

var ErrComputeBalance = errors.New("insufficient platform balance for the maximum task cost")
var ErrComputePrice = errors.New("a price has not been configured for this hardware")

type ComputePricing struct {
	Enabled bool                       `json:"enabled"`
	Version int64                      `json:"version"`
	Rates   map[string]decimal.Decimal `json:"rates"`
}

func (s *ColabService) Pricing(ctx context.Context) (*ComputePricing, error) {
	var p ComputePricing
	var raw []byte
	e := s.store.db.QueryRowContext(ctx, `SELECT enabled,version,rates FROM compute_pricing WHERE id=1`).Scan(&p.Enabled, &p.Version, &raw)
	if e != nil {
		return nil, e
	}
	e = json.Unmarshal(raw, &p.Rates)
	return &p, e
}
func (s *ColabService) SetPricing(ctx context.Context, p ComputePricing) error {
	if p.Rates == nil {
		p.Rates = map[string]decimal.Decimal{}
	}
	if len(p.Rates) > 8 {
		return os.ErrInvalid
	}
	for k, v := range p.Rates {
		if hw, ok := colab.LookupHardware(k); !ok || hw.ID != k || v.IsNegative() || v.GreaterThan(decimal.NewFromInt(1000000)) || !v.Equal(v.Round(6)) {
			return os.ErrInvalid
		}
	}
	raw, e := json.Marshal(p.Rates)
	if e != nil {
		return e
	}
	r, e := s.store.db.ExecContext(ctx, `UPDATE compute_pricing SET enabled=$1,rates=$2,version=version+1 WHERE id=1 AND version=$3`, p.Enabled, string(raw), p.Version)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrComputeConflict
	}
	return nil
}
func computeCost(rate decimal.Decimal, seconds int) decimal.Decimal {
	return rate.Mul(decimal.NewFromInt(int64(max(0, seconds)))).DivRound(decimal.NewFromInt(3600), 12).RoundCeil(8)
}
func reserveCompute(ctx context.Context, tx *sql.Tx, user int64, hardware string, timeout int) (decimal.Decimal, decimal.Decimal, string, error) {
	var enabled bool
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT enabled,rates FROM compute_pricing WHERE id=1 FOR SHARE`).Scan(&enabled, &raw)
	zero := decimal.Zero
	if e != nil {
		return zero, zero, "", e
	}
	if !enabled {
		return zero, zero, "free", nil
	}
	var rates map[string]decimal.Decimal
	if json.Unmarshal(raw, &rates) != nil {
		return zero, zero, "", ErrComputePrice
	}
	rate, ok := rates[hardware]
	if !ok {
		return zero, zero, "", ErrComputePrice
	}
	hold := computeCost(rate, timeout)
	r, e := tx.ExecContext(ctx, `UPDATE users SET balance=balance-$2 WHERE id=$1 AND balance>=$2 AND deleted_at IS NULL`, user, hold)
	if e != nil {
		return zero, zero, "", e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return zero, zero, "", ErrComputeBalance
	}
	return rate, hold, "reserved", nil
}
func (s *ColabService) invalidateComputeBalance(user int64) {
	if s.users == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.users.authCacheInvalidator != nil {
		s.users.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, user)
	}
	if s.users.billingCache != nil {
		_ = s.users.billingCache.InvalidateUserBalance(ctx, user)
	}
}

// Settlement is retriable and atomic. Cleanup must finish first; queued cancellations
// and allocations rejected before execution refund the entire reservation.
func (s *ColabService) settleCompute(ctx context.Context, id string) error {
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var user int64
	var rate, hold decimal.Decimal
	var seconds int
	e = tx.QueryRowContext(ctx, `SELECT user_id,price_per_hour,reserved_cost,
 CASE WHEN allocated_at IS NULL THEN 0 ELSE LEAST(timeout_seconds,GREATEST(0,CEIL(EXTRACT(EPOCH FROM (released_at-allocated_at)))::int)) END
 FROM colab_jobs WHERE id=$1 AND billing_status='reserved'
 AND status IN ('succeeded','failed','cancelled','unknown') AND (runtime_released OR runtime_name='') FOR UPDATE`, id).Scan(&user, &rate, &hold, &seconds)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	cost := decimal.Min(hold, computeCost(rate, seconds))
	if _, e = tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2 WHERE id=$1`, user, hold.Sub(cost)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE colab_jobs SET charged_cost=$2,billable_seconds=$3,billing_status='settled' WHERE id=$1`, id, cost, seconds); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		s.invalidateComputeBalance(user)
	}
	return e
}
func (s *ColabService) settleCompleted(ctx context.Context) {
	rows, e := s.store.db.QueryContext(ctx, `SELECT id FROM colab_jobs WHERE billing_status='reserved' AND status IN ('succeeded','failed','cancelled','unknown') AND (runtime_released OR runtime_name='')`)
	if e != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.settleCompute(ctx, id)
	}
}
