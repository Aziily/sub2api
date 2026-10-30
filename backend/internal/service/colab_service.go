package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/colab"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googledrive"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrComputeUnavailable = errors.New("compute is not configured or has no verified CPU account")
var ErrComputeHardwareUnavailable = errors.New("requested GPU/CPU hardware is not available on any enabled Colab account")
var ErrComputeConflict = errors.New("job id already exists with different content, or queue is full")

type ColabService struct {
	users    *UserService
	store    *DriveStore
	drive    *GoogleDriveService
	flow     *googledrive.OAuthFlow
	redirect string
	disabled string
}
type ComputeAccount struct {
	Consumption          *colab.Consumption     `json:"consumption"`
	ConsumptionCheckedAt *time.Time             `json:"consumption_checked_at"`
	ConsumptionError     string                 `json:"consumption_error"`
	ID                   int64                  `json:"id"`
	Email                string                 `json:"email"`
	Enabled              bool                   `json:"enabled"`
	Subscription         json.RawMessage        `json:"subscription"`
	Specs                json.RawMessage        `json:"runtime_specs"`
	HardwareSpecs        []colab.HardwareOption `json:"hardware_specs"`
	JobCount             int                    `json:"job_count"`
	TotalRuntimeSeconds  int                    `json:"total_runtime_seconds"`
	GPURuntimeSeconds    int                    `json:"gpu_runtime_seconds"`
	A100RuntimeSeconds   int                    `json:"a100_runtime_seconds"`
	LastError            string                 `json:"last_error"`
	CheckedAt            *time.Time             `json:"checked_at"`
}
type ComputeJob struct {
	PricePerHour    decimal.Decimal `json:"price_per_hour"`
	ReservedCost    decimal.Decimal `json:"reserved_cost"`
	ChargedCost     decimal.Decimal `json:"charged_cost"`
	BillableSeconds int             `json:"billable_seconds"`
	BillingStatus   string          `json:"billing_status"`
	ID              string          `json:"id"`
	UserID          int64           `json:"user_id"`
	AccountID       *int64          `json:"account_id"`
	Title           string          `json:"title"`
	Code            string          `json:"code"`
	Hardware        string          `json:"hardware"`
	Status          string          `json:"status"`
	Timeout         int             `json:"timeout_seconds"`
	DurationSeconds int             `json:"duration_seconds"`
	AllocatedAt     *time.Time      `json:"allocated_at"`
	ReleasedAt      *time.Time      `json:"released_at"`
	Workspace       json.RawMessage `json:"workspace,omitempty"`
	Runtime         string          `json:"runtime_name"`
	Operation       string          `json:"operation_name"`
	Released        bool            `json:"runtime_released"`
	CancelRequested bool            `json:"cancel_requested"`
	Logs            string          `json:"logs"`
	Error           string          `json:"error"`
	OutputPath      string          `json:"output_path"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

const jobColumns = `id,user_id,account_id,title,code,hardware,status,timeout_seconds,runtime_name,operation_name,runtime_released,cancel_requested,logs,error,output_path,created_at,updated_at,allocated_at,released_at,workspace,price_per_hour,reserved_cost,charged_cost,billable_seconds,billing_status`

func scanComputeJob(row interface{ Scan(...any) error }) (*ComputeJob, error) {
	var j ComputeJob
	var workspace []byte
	e := row.Scan(&j.ID, &j.UserID, &j.AccountID, &j.Title, &j.Code, &j.Hardware, &j.Status, &j.Timeout, &j.Runtime, &j.Operation, &j.Released, &j.CancelRequested, &j.Logs, &j.Error, &j.OutputPath, &j.CreatedAt, &j.UpdatedAt, &j.AllocatedAt, &j.ReleasedAt, &workspace, &j.PricePerHour, &j.ReservedCost, &j.ChargedCost, &j.BillableSeconds, &j.BillingStatus)
	j.Workspace = workspace
	if j.Hardware == "" {
		j.Hardware = "CPU"
	}
	if j.AllocatedAt != nil {
		end := time.Now()
		if j.ReleasedAt != nil {
			end = *j.ReleasedAt
		}
		j.DurationSeconds = max(0, int(end.Sub(*j.AllocatedAt).Seconds()))
	}
	return &j, e
}
func ProvideColabService(store *DriveStore, drive *GoogleDriveService, cfg *config.Config, users *UserService) *ColabService {
	s := &ColabService{users: users, store: store, drive: drive, redirect: os.Getenv("COLAB_REDIRECT_URI")}
	if cfg == nil || !cfg.Totp.EncryptionKeyConfigured {
		s.disabled = "Persistent credential encryption is not configured"
		return s
	}
	id, secret := os.Getenv("COLAB_CLIENT_ID"), os.Getenv("COLAB_CLIENT_SECRET")
	if id == "" {
		id = os.Getenv("GOOGLE_DRIVE_CLIENT_ID")
		secret = os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")
	}
	flow, e := googledrive.NewOAuthFlow(googledrive.OAuthConfig{ClientID: id, ClientSecret: secret, RedirectURI: s.redirect, Scopes: []string{colab.Scope, "openid", "email"}}, nil)
	if e != nil {
		s.disabled = "Colab OAuth client and HTTPS callback must be configured"
		return s
	}
	s.flow = flow
	go s.workerLoop()
	return s
}
func (s *ColabService) Authorize(actor int64) (string, error) {
	if s.flow == nil {
		return "", ErrComputeUnavailable
	}
	return s.flow.Start(actor, 0)
}
func (s *ColabService) Callback(ctx context.Context, state, code string) error {
	if s.flow == nil {
		return ErrComputeUnavailable
	}
	_, _, t, e := s.flow.ExchangeByState(ctx, state, code)
	if e != nil {
		return e
	}
	if !strings.Contains(" "+t.Scope+" ", " "+colab.Scope+" ") || t.RefreshToken == "" {
		return errors.New("Colab offline permission was not granted")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://openidconnect.googleapis.com/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	r, e := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if e != nil {
		return errors.New("Google identity check failed")
	}
	defer r.Body.Close()
	var identity struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if r.StatusCode != 200 {
		return errors.New("Google identity check rejected")
	}
	if e = json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&identity); e != nil {
		return e
	}
	if !identity.Verified || identity.Email == "" {
		return errors.New("Google email is not verified")
	}
	enc, e := s.store.encryptor.Encrypt(t.RefreshToken)
	if e != nil {
		return e
	}
	var id int64
	e = s.store.db.QueryRowContext(ctx, `INSERT INTO colab_accounts(email,refresh_token_encrypted) VALUES(lower($1),$2) ON CONFLICT(email) DO UPDATE SET refresh_token_encrypted=EXCLUDED.refresh_token_encrypted RETURNING id`, identity.Email, enc).Scan(&id)
	if e != nil {
		return e
	}
	return s.RefreshAccount(ctx, id)
}
func (s *ColabService) client(ctx context.Context, id int64) (*colab.Client, error) {
	if s.flow == nil {
		return nil, ErrComputeUnavailable
	}
	var enc string
	if e := s.store.db.QueryRowContext(ctx, `SELECT refresh_token_encrypted FROM colab_accounts WHERE id=$1`, id).Scan(&enc); e != nil {
		return nil, e
	}
	refresh, e := s.store.encryptor.Decrypt(enc)
	if e != nil {
		return nil, e
	}
	t, e := s.flow.Refresh(ctx, refresh)
	if e != nil {
		return nil, errors.New("Colab credential refresh failed; reconnect this account")
	}
	if t.RefreshToken != "" && t.RefreshToken != refresh {
		enc, e = s.store.encryptor.Encrypt(t.RefreshToken)
		if e != nil {
			return nil, e
		}
		if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET refresh_token_encrypted=$2 WHERE id=$1`, id, enc); e != nil {
			return nil, e
		}
	}
	return &colab.Client{Token: t.AccessToken}, nil
}
func (s *ColabService) RefreshAccount(ctx context.Context, id int64) error {
	c, e := s.client(ctx, id)
	if e == nil {
		s.refreshConsumption(ctx, id, c)
	}
	var sub, spec json.RawMessage
	if e == nil {
		e = c.JSON(ctx, "GET", "/v1beta/subscription", nil, &sub)
	}
	if e == nil {
		e = c.JSON(ctx, "GET", "/v1beta/runtimespecs", nil, &spec)
	}
	if e == nil && !colab.CPUEligible(spec) {
		e = errors.New("Google does not permit a standard CPU runtime for this account")
	}
	if e != nil {
		_, _ = s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET last_error=$2,checked_at=now() WHERE id=$1`, id, e.Error())
		return e
	}
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET subscription=$2,runtime_specs=$3,last_error='',checked_at=now() WHERE id=$1`, id, string(sub), string(spec)); e != nil {
		return e
	}
	return nil
}
func (s *ColabService) Accounts(ctx context.Context) ([]ComputeAccount, error) {
	rows, e := s.store.db.QueryContext(ctx, `SELECT id,email,enabled,subscription,runtime_specs,last_error,checked_at,consumption,consumption_checked_at,consumption_error FROM colab_accounts ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComputeAccount{}
	for rows.Next() {
		var a ComputeAccount
		var consumption []byte
		if e = rows.Scan(&a.ID, &a.Email, &a.Enabled, &a.Subscription, &a.Specs, &a.LastError, &a.CheckedAt, &consumption, &a.ConsumptionCheckedAt, &a.ConsumptionError); e != nil {
			return nil, e
		}
		if len(consumption) > 0 {
			_ = json.Unmarshal(consumption, &a.Consumption)
		}
		a.HardwareSpecs = colab.SummarizeSpecs(a.Specs)
		out = append(out, a)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	uRows, e := s.store.db.QueryContext(ctx, `SELECT account_id, COUNT(*), COALESCE(SUM(GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int)), 0), COALESCE(SUM(CASE WHEN hardware NOT IN ('CPU','CPU_HIGHMEM') THEN GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int) ELSE 0 END), 0), COALESCE(SUM(CASE WHEN hardware IN ('A100','A100_HIGHMEM') THEN GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int) ELSE 0 END), 0) FROM colab_jobs WHERE account_id IS NOT NULL GROUP BY account_id`)
	if e != nil {
		return nil, e
	}
	if e == nil {
		defer uRows.Close()
		byID := map[int64]int{}
		for i := range out {
			byID[out[i].ID] = i
		}
		for uRows.Next() {
			var aid int64
			var cnt, tot, gpu, a100 int
			if e = uRows.Scan(&aid, &cnt, &tot, &gpu, &a100); e != nil {
				return nil, e
			} else {
				if idx, ok := byID[aid]; ok {
					out[idx].JobCount = cnt
					out[idx].TotalRuntimeSeconds = tot
					out[idx].GPURuntimeSeconds = gpu
					out[idx].A100RuntimeSeconds = a100
				}
			}
		}
	}
	return out, uRows.Err()
}
func (s *ColabService) Status(ctx context.Context, user int64, admin bool) (map[string]any, error) {
	a, e := s.Accounts(ctx)
	if e != nil {
		return nil, e
	}
	ready := false
	poolSpecs := colab.SummarizeSpecs(nil)
	for _, v := range a {
		if v.Enabled && v.LastError == "" && v.CheckedAt != nil {
			ready = true
			for i, hs := range v.HardwareSpecs {
				if hs.Eligible && i < len(poolSpecs) {
					poolSpecs[i].Eligible = true
				}
			}
		}
	}
	hwNames := []string{}
	for _, hs := range poolSpecs {
		if hs.Eligible && !hs.HighMem {
			hwNames = append(hwNames, hs.ID)
		}
	}
	hwLabel := "CPU"
	if len(hwNames) > 0 {
		hwLabel = strings.Join(hwNames, " / ")
	}
	var totalJobs, succeededJobs, totalSec, gpuSec, a100Sec int
	var queuedJobs, runningJobs, failedJobs int
	var chargedCost, reservedCost decimal.Decimal
	e = s.store.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END), 0), COALESCE(SUM(GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int)), 0), COALESCE(SUM(CASE WHEN hardware NOT IN ('CPU','CPU_HIGHMEM') THEN GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int) ELSE 0 END), 0), COALESCE(SUM(CASE WHEN hardware IN ('A100','A100_HIGHMEM') THEN GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(released_at, now()) - allocated_at))::int) ELSE 0 END), 0), COUNT(*) FILTER (WHERE status='queued'), COUNT(*) FILTER (WHERE status IN ('provisioning','running','writing')), COUNT(*) FILTER (WHERE status IN ('failed','unknown')), COALESCE(SUM(charged_cost),0), COALESCE(SUM(reserved_cost) FILTER (WHERE billing_status='reserved'),0) FROM colab_jobs WHERE user_id=$1 OR $2`, user, admin).Scan(&totalJobs, &succeededJobs, &totalSec, &gpuSec, &a100Sec, &queuedJobs, &runningJobs, &failedJobs, &chargedCost, &reservedCost)
	if e != nil {
		return nil, e
	}
	var credits float64
	if e = s.store.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, user).Scan(&credits); e != nil {
		return nil, e
	}
	pricing, e := s.Pricing(ctx)
	if e != nil {
		return nil, e
	}
	var storage *DriveOverview
	if s.drive.Enabled() {
		storage, e = s.drive.Overview(ctx, user)
		if e != nil {
			return nil, e
		}
	}
	storageReady := storage != nil && storage.Enabled && (storage.AvailableBytes == nil || *storage.AvailableBytes > 0)
	usageSummary := map[string]any{
		"total_jobs":            totalJobs,
		"queued_jobs":           queuedJobs,
		"running_jobs":          runningJobs,
		"failed_jobs":           failedJobs,
		"charged_cost":          chargedCost,
		"reserved_cost":         reservedCost,
		"succeeded_jobs":        succeededJobs,
		"total_runtime_seconds": totalSec,
		"gpu_runtime_seconds":   gpuSec,
		"a100_runtime_seconds":  a100Sec,
	}
	out := map[string]any{
		"configured":              s.flow != nil,
		"ready":                   ready && storageReady,
		"disabled_reason":         s.disabled,
		"hardware":                hwLabel,
		"hardware_options":        poolSpecs,
		"usage_summary":           usageSummary,
		"max_timeout_seconds":     3600,
		"compute_units":           nil,
		"platform_credits":        credits,
		"compute_billing_enabled": pricing.Enabled,
		"pricing":                 pricing, "storage": storage,
	}
	if admin {
		out["accounts"] = a
		var total float64
		var known int
		for _, account := range a {
			if account.Consumption != nil && account.Consumption.Balance != nil {
				total += *account.Consumption.Balance
				known++
			}
		}
		if known > 0 {
			out["compute_units"] = total
		}
		out["cu_accounts_known"] = known
		out["redirect_uri"] = s.redirect
	}
	return out, nil
}
func (s *ColabService) SetEnabled(ctx context.Context, id int64, on bool) error {
	r, e := s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET enabled=$2 WHERE id=$1`, id, on)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *ColabService) Submit(ctx context.Context, user int64, id, title, code, hardware string, timeout int, workspaces ...*ComputeWorkspace) (*ComputeJob, error) {
	if s.flow == nil || !s.drive.Enabled() {
		return nil, ErrComputeUnavailable
	}
	var ws *ComputeWorkspace
	if len(workspaces) > 0 {
		ws = workspaces[0]
	}
	var workspaceJSON []byte
	if ws != nil {
		if e := s.validateWorkspace(ctx, user, ws); e != nil {
			return nil, e
		}
		workspaceJSON, _ = json.Marshal(ws)
		code = "# Workspace task: " + strings.Join(ws.Command, " ")
	}
	hwSpec, hwOK := colab.LookupHardware(hardware)
	if _, e := uuid.Parse(id); e != nil || !hwOK || len(code) == 0 || len(code) > 65536 || len(title) > 120 || timeout < 30 || timeout > 3600 {
		return nil, os.ErrInvalid
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", title, code+string(workspaceJSON), hwSpec.ID, timeout)))
	hs := hex.EncodeToString(hash[:])
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(25220261031)`); e != nil {
		return nil, e
	}
	var existing string
	var owner int64
	e = tx.QueryRowContext(ctx, `SELECT user_id,request_hash FROM colab_jobs WHERE id=$1`, id).Scan(&owner, &existing)
	if e == nil {
		if owner != user || hs != existing {
			return nil, ErrComputeConflict
		}
		_ = tx.Rollback()
		return s.Job(ctx, user, id, false)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	storage, e := s.drive.Overview(ctx, user)
	if e != nil {
		return nil, e
	}
	if !storage.Enabled || (storage.AvailableBytes != nil && *storage.AvailableBytes <= 0) {
		return nil, ErrDriveQuotaExceeded
	}
	specRows, e := tx.QueryContext(ctx, `SELECT runtime_specs FROM colab_accounts WHERE enabled AND last_error='' AND checked_at IS NOT NULL`)
	if e != nil {
		return nil, e
	}
	anyAccount := false
	hwEligible := false
	for specRows.Next() {
		var raw []byte
		if specRows.Scan(&raw) == nil {
			anyAccount = true
			if colab.HardwareEligible(raw, hwSpec.ID) {
				hwEligible = true
			}
		}
	}
	specRows.Close()
	if !anyAccount {
		return nil, ErrComputeUnavailable
	}
	if !hwEligible {
		return nil, ErrComputeHardwareUnavailable
	}
	var queued int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM colab_jobs WHERE status IN ('queued','provisioning','running','writing')`).Scan(&queued); e != nil {
		return nil, e
	}
	if queued >= 20 {
		return nil, ErrComputeConflict
	}
	rate, hold, billing, e := reserveCompute(ctx, tx, user, hwSpec.ID, timeout)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO colab_jobs(id,user_id,title,code,hardware,request_hash,timeout_seconds,workspace,price_per_hour,reserved_cost,billing_status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, user, title, code, hwSpec.ID, hs, timeout, nullableWorkspace(workspaceJSON), rate, hold, billing); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	s.invalidateComputeBalance(user)
	return s.Job(ctx, user, id, false)
}
func (s *ColabService) Job(ctx context.Context, user int64, id string, admin bool) (*ComputeJob, error) {
	if _, e := uuid.Parse(id); e != nil {
		return nil, sql.ErrNoRows
	}
	j, e := scanComputeJob(s.store.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM colab_jobs WHERE id=$1 AND (user_id=$2 OR $3)`, id, user, admin))
	if !admin {
		privateComputeJob(j)
	}
	return j, e
}
func (s *ColabService) Jobs(ctx context.Context, user int64, admin bool) ([]ComputeJob, error) {
	rows, e := s.store.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM colab_jobs WHERE user_id=$1 OR $2 ORDER BY created_at DESC LIMIT 100`, user, admin)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComputeJob{}
	for rows.Next() {
		j, e := scanComputeJob(rows)
		if e != nil {
			return nil, e
		}
		if !admin {
			privateComputeJob(j)
		}
		j.Code = ""
		j.Logs = ""
		out = append(out, *j)
	}
	return out, rows.Err()
}
func (s *ColabService) Cancel(ctx context.Context, user int64, id string, admin bool) error {
	if _, e := s.Job(ctx, user, id, admin); e != nil {
		return e
	}
	_, e := s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET cancel_requested=true,status=CASE WHEN status='queued' THEN 'cancelled' ELSE status END,updated_at=now() WHERE id=$1 AND status IN ('queued','provisioning','running','writing')`, id)
	return e
}

// One global, database-held lock bounds this initial CPU pool to one runtime.
// A lost worker never re-executes code: its uncertain job is reconciled and
// the persisted deterministic runtime name is deleted before any next job.
func (s *ColabService) workerLoop() {
	for {
		s.workerTick()
		time.Sleep(3 * time.Second)
	}
}
func (s *ColabService) workerTick() {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Minute)
	defer cancel()
	conn, e := s.store.db.Conn(ctx)
	if e != nil {
		return
	}
	defer conn.Close()
	var locked bool
	if e = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(25220261030)`).Scan(&locked); e != nil || !locked {
		return
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(25220261030)`)
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if conn.PingContext(ctx) != nil {
					cancel()
					return
				}
			}
		}
	}()
	// Only a lock holder can recover jobs left by a crashed process.
	_, e = conn.ExecContext(ctx, `UPDATE colab_jobs SET status='unknown',error='Worker restarted; code will not be replayed',updated_at=now() WHERE status IN ('provisioning','running','writing')`)
	if e != nil {
		return
	}
	rows, e := conn.QueryContext(ctx, `SELECT id,account_id,runtime_name,operation_name,hardware FROM colab_jobs WHERE runtime_name<>'' AND NOT runtime_released`)
	if e != nil {
		return
	}
	type pending struct {
		id                           string
		account                      int64
		runtime, operation, hardware string
	}
	left := []pending{}
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.account, &p.runtime, &p.operation, &p.hardware) != nil {
			rows.Close()
			return
		}
		left = append(left, p)
	}
	rows.Close()
	for _, p := range left {
		if !s.release(ctx, p.account, p.runtime, p.operation, p.hardware) {
			return
		}
		if _, e = conn.ExecContext(ctx, `UPDATE colab_jobs SET runtime_released=true,released_at=now(),input_token_hash='' WHERE id=$1`, p.id); e != nil {
			return
		}
	}
	s.settleCompleted(ctx)
	var id, hw string
	e = conn.QueryRowContext(ctx, `SELECT id,hardware FROM colab_jobs WHERE status='queued' AND NOT cancel_requested ORDER BY created_at LIMIT 1`).Scan(&id, &hw)
	if e != nil {
		return
	}
	accRows, e := conn.QueryContext(ctx, `SELECT id,runtime_specs FROM colab_accounts WHERE enabled AND last_error='' AND checked_at IS NOT NULL ORDER BY id`)
	if e != nil {
		return
	}
	var account int64
	for accRows.Next() {
		var aid int64
		var raw []byte
		if accRows.Scan(&aid, &raw) == nil && colab.HardwareEligible(raw, hw) {
			account = aid
			break
		}
	}
	accRows.Close()
	if account == 0 {
		_, _ = conn.ExecContext(ctx, `UPDATE colab_jobs SET status='failed',error='No enabled Colab account is eligible for requested hardware',updated_at=now() WHERE id=$1`, id)
		return
	}
	runtime := "runtimes/pool-" + id
	r, e := conn.ExecContext(ctx, `UPDATE colab_jobs SET status='provisioning',account_id=$2,runtime_name=$3,updated_at=now() WHERE id=$1 AND status='queued' AND NOT cancel_requested`, id, account, runtime)
	if e != nil {
		return
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return
	}
	j, e := s.Job(ctx, 0, id, true)
	if e != nil {
		return
	}
	s.run(ctx, j)
}
func (s *ColabService) release(ctx context.Context, account int64, runtime, operation, hardware string) bool {
	c, e := s.client(ctx, account)
	if e != nil {
		return false
	}
	if operation == "pending" {
		// Recover the *same* idempotent allocation after a lost response.
		// A 404 for its runtime alone does not prove provisioning never started.
		id := strings.TrimPrefix(runtime, "runtimes/pool-")
		var op colab.Operation
		if c.JSON(ctx, "POST", allocationPath(runtime, id), colab.RuntimeBody(hardware), &op) != nil {
			return false
		}
		operation = op.Name
		if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET operation_name=$2 WHERE runtime_name=$1`, runtime, operation); e != nil {
			return false
		}
		if !op.Done {
			return false
		}
	}
	if operation != "" {
		var op colab.Operation
		if c.JSON(ctx, "GET", "/v1/"+operation, nil, &op) != nil || !op.Done {
			return false
		}
	}
	e = c.JSON(ctx, "DELETE", "/v1beta/"+runtime, nil, nil)
	if e == nil {
		return true
	}
	var api *colab.APIError
	return errors.As(e, &api) && api.Status == 404
}
func (s *ColabService) run(parent context.Context, j *ComputeJob) {
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.settleCompute(ctx, j.ID)
		if c, e := s.client(ctx, *j.AccountID); e == nil {
			s.refreshConsumption(ctx, *j.AccountID, c)
		}
	}()
	ctx, cancel := context.WithTimeout(parent, time.Duration(j.Timeout)*time.Second)
	defer cancel()
	// Cancellation is durable and is noticed even when an upstream call blocks.
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				var stop bool
				if s.store.db.QueryRowContext(ctx, `SELECT cancel_requested FROM colab_jobs WHERE id=$1`, j.ID).Scan(&stop) != nil || stop {
					cancel()
					return
				}
			}
		}
	}()
	err := s.execute(ctx, j)
	final := "succeeded"
	message := ""
	if err != nil {
		final = "failed"
		message = err.Error()
	}
	if ctx.Err() != nil {
		final = "failed"
		message = "Job deadline reached"
	}
	var stopped bool
	_ = s.store.db.QueryRowContext(parent, `SELECT cancel_requested FROM colab_jobs WHERE id=$1`, j.ID).Scan(&stopped)
	if stopped {
		final = "cancelled"
		message = "Cancelled by user"
	}
	_, _ = s.store.db.ExecContext(parent, `UPDATE colab_jobs SET status=$2,error=$3,updated_at=now() WHERE id=$1`, j.ID, final, message)
	cleanup, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	if !j.Released && s.release(cleanup, *j.AccountID, j.Runtime, j.Operation, j.Hardware) {
		_, _ = s.store.db.ExecContext(cleanup, `UPDATE colab_jobs SET runtime_released=true,released_at=now(),input_token_hash='' WHERE id=$1`, j.ID)
	}
}
func (s *ColabService) execute(ctx context.Context, j *ComputeJob) error {
	c, e := s.client(ctx, *j.AccountID)
	if e != nil {
		return e
	}
	var op colab.Operation
	j.Operation = "pending"
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET operation_name='pending' WHERE id=$1`, j.ID); e != nil {
		return e
	}
	e = c.JSON(ctx, "POST", allocationPath(j.Runtime, j.ID), colab.RuntimeBody(j.Hardware), &op)
	if e != nil {
		var api *colab.APIError
		if errors.As(e, &api) && api.Status >= 400 && api.Status < 500 && api.Status != 409 {
			j.Released = true
			_, _ = s.store.db.ExecContext(context.Background(), `UPDATE colab_jobs SET runtime_released=true,released_at=now(),input_token_hash='' WHERE id=$1`, j.ID)
		}
		return e
	}
	j.Operation = op.Name
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET operation_name=$2 WHERE id=$1`, j.ID, op.Name); e != nil {
		return e
	}
	for !op.Done {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		if !strings.HasPrefix(op.Name, "operations/") {
			return errors.New("Invalid Colab operation name")
		}
		if e = c.JSON(ctx, "GET", "/v1/"+op.Name, nil, &op); e != nil {
			return e
		}
	}
	if len(op.Error) > 0 && string(op.Error) != "null" {
		return fmt.Errorf("Colab could not allocate %s runtime", j.Hardware)
	}
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET status='running',allocated_at=now(),updated_at=now() WHERE id=$1`, j.ID); e != nil {
		return e
	}
	executionCode := j.Code
	if len(j.Workspace) > 0 && string(j.Workspace) != "null" {
		executionCode, e = s.workspaceCode(ctx, j)
		if e != nil {
			return e
		}
	}
	result, e := c.Execute(ctx, j.Runtime, j.ID, executionCode)
	if e != nil {
		return e
	}
	logs := result.Stdout + result.Stderr
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET status='writing',logs=$2,updated_at=now() WHERE id=$1`, j.ID, logs); e != nil {
		return e
	}
	// Store the provider's captured output, not a synthetic local execution.
	dir := "/Colab-" + j.ID
	if e = s.drive.Mkdir(ctx, j.UserID, dir); e != nil && !errors.Is(e, os.ErrExist) {
		return e
	}
	if len(j.Workspace) > 0 && string(j.Workspace) != "null" {
		if e = s.saveWorkspaceOutput(ctx, c, j, dir); e != nil {
			return e
		}
	}
	raw, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return e
	}
	for name, data := range map[string][]byte{"execution.json": raw, "stdout.txt": []byte(result.Stdout), "stderr.txt": []byte(result.Stderr)} {
		if e = s.drive.Upload(ctx, j.UserID, dir+"/"+name, "text/plain", int64(len(data)), bytes.NewReader(data)); e != nil {
			return fmt.Errorf("Drive output writeback failed: %w", e)
		}
	}
	if _, e = s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET output_path=$2 WHERE id=$1`, j.ID, dir); e != nil {
		return e
	}
	if len(result.ExecutionError) > 0 && string(result.ExecutionError) != "null" {
		return errors.New("Python execution failed; see logs and execution.json")
	}
	return nil
}

func allocationPath(runtime, id string) string {
	return "/v1beta/runtimes?runtimeId=" + strings.TrimPrefix(runtime, "runtimes/") + "&requestId=" + id
}

func privateComputeJob(j *ComputeJob) {
	if j != nil {
		j.AccountID = nil
		j.Runtime = ""
		j.Operation = ""
	}
}

func (s *ColabService) refreshConsumption(ctx context.Context, id int64, c *colab.Client) {
	v, e := c.Consumption(ctx)
	if e != nil {
		_, _ = s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET consumption_error=$2 WHERE id=$1`, id, e.Error())
		return
	}
	raw, _ := json.Marshal(v)
	_, _ = s.store.db.ExecContext(ctx, `UPDATE colab_accounts SET consumption=$2,consumption_checked_at=now(),consumption_error='' WHERE id=$1`, id, string(raw))
}
