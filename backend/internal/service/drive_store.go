package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lib/pq"
)

// DriveStorageAccount is one Google account in the storage pool. Credentials
// are never part of this struct.
type DriveStorageAccount struct {
	ID             int64      `json:"id"`
	GoogleEmail    string     `json:"google_email"`
	Enabled        bool       `json:"enabled"`
	QuotaLimit     *int64     `json:"quota_limit"`
	QuotaUsage     *int64     `json:"quota_usage"`
	QuotaCheckedAt *time.Time `json:"quota_checked_at"`
	LastError      string     `json:"last_error"`
	FileCount      int64      `json:"file_count"`
	StoredBytes    int64      `json:"stored_bytes"`
	ReservedBytes  int64      `json:"reserved_bytes"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RootFolderID   string     `json:"-"`
	ImportUserID   int64      `json:"-"`
}

// Free returns the bytes still available for new files, or -1 if Google
// reports no limit. Reservations of in-flight uploads are subtracted.
func (a *DriveStorageAccount) Free() int64 {
	if a.QuotaLimit == nil {
		return -1
	}
	used := a.ReservedBytes
	if a.QuotaUsage != nil {
		used += *a.QuotaUsage
	}
	if free := *a.QuotaLimit - used; free > 0 {
		return free
	}
	return 0
}

// Usable reports whether new files may be placed in this account.
func (a *DriveStorageAccount) Usable() bool {
	return a.Enabled && a.LastError == "" && a.QuotaCheckedAt != nil
}

// DriveNode is one entry of a user's virtual tree. ParentID 0 is the root.
type DriveNode struct {
	ID               int64
	UserID           int64
	ParentID         int64
	Name             string
	IsDir            bool
	Size             int64
	MimeType         string
	StorageAccountID int64
	GoogleFileID     string
	State            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DriveUsage summarizes one user's tree.
type DriveUsage struct {
	UsedBytes     int64  `json:"used_bytes"`
	ReservedBytes int64  `json:"reserved_bytes"`
	FileCount     int64  `json:"file_count"`
	FolderCount   int64  `json:"folder_count"`
	QuotaBytes    *int64 `json:"quota_bytes"`
}

// DriveUserUsage is the administrator view of one user's usage.
type DriveUserUsage struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	DriveUsage
}

// DriveStore persists the storage pool and every user's virtual tree.
type DriveStore struct {
	db        *sql.DB
	encryptor SecretEncryptor
}

func NewDriveStore(db *sql.DB, encryptor SecretEncryptor) *DriveStore {
	return &DriveStore{db: db, encryptor: encryptor}
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func nullParent(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// ---- storage accounts ----

const accountSelect = `SELECT a.id,a.google_email,a.enabled,a.quota_limit,a.quota_usage,a.quota_checked_at,a.last_error,
        a.root_folder_id,COALESCE(a.import_user_id,0),a.created_at,a.updated_at,
        COALESCE(s.files,0),COALESCE(s.stored,0),COALESCE(s.reserved,0)
    FROM drive_storage_accounts a LEFT JOIN (
        SELECT storage_account_id,
            COUNT(*) FILTER (WHERE state='ready') AS files,
            COALESCE(SUM(size) FILTER (WHERE state='ready'),0) AS stored,
            COALESCE(SUM(size) FILTER (WHERE state='pending' AND pending_expires_at>NOW()),0) AS reserved
        FROM drive_nodes WHERE storage_account_id IS NOT NULL GROUP BY storage_account_id
    ) s ON s.storage_account_id=a.id`

func scanAccount(row interface{ Scan(...any) error }) (*DriveStorageAccount, error) {
	var a DriveStorageAccount
	var limit, usage sql.NullInt64
	var checked sql.NullTime
	if err := row.Scan(&a.ID, &a.GoogleEmail, &a.Enabled, &limit, &usage, &checked, &a.LastError, &a.RootFolderID,
		&a.ImportUserID, &a.CreatedAt, &a.UpdatedAt, &a.FileCount, &a.StoredBytes, &a.ReservedBytes); err != nil {
		return nil, err
	}
	if limit.Valid {
		a.QuotaLimit = &limit.Int64
	}
	if usage.Valid {
		a.QuotaUsage = &usage.Int64
	}
	if checked.Valid {
		a.QuotaCheckedAt = &checked.Time
	}
	return &a, nil
}

func (s *DriveStore) ListAccounts(ctx context.Context) ([]*DriveStorageAccount, error) {
	rows, err := s.db.QueryContext(ctx, accountSelect+` ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DriveStorageAccount{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *DriveStore) Account(ctx context.Context, id int64) (*DriveStorageAccount, error) {
	return scanAccount(s.db.QueryRowContext(ctx, accountSelect+` WHERE a.id=$1`, id))
}

func (s *DriveStore) AccountByEmail(ctx context.Context, email string) (*DriveStorageAccount, error) {
	return scanAccount(s.db.QueryRowContext(ctx, accountSelect+` WHERE lower(a.google_email)=lower($1)`, email))
}

// SaveAccount adds a storage account or replaces the credential of the
// account with the same Google email. It returns the account ID.
func (s *DriveStore) SaveAccount(ctx context.Context, email, refreshToken, rootFolderID string) (int64, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 320 || strings.TrimSpace(refreshToken) == "" || strings.TrimSpace(rootFolderID) == "" {
		return 0, errors.New("invalid storage account")
	}
	encrypted, err := s.encryptor.Encrypt(refreshToken)
	if err != nil {
		return 0, errors.New("cannot encrypt Drive credential")
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO drive_storage_accounts (google_email,refresh_token_encrypted,root_folder_id)
        VALUES ($1,$2,$3) ON CONFLICT ((lower(google_email))) DO UPDATE SET
        refresh_token_encrypted=EXCLUDED.refresh_token_encrypted,root_folder_id=EXCLUDED.root_folder_id,
        last_error='',updated_at=NOW() RETURNING id`, email, encrypted, rootFolderID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("save storage account: %w", err)
	}
	return id, nil
}

// RefreshToken is strictly server-side and must never be serialized or logged.
func (s *DriveStore) RefreshToken(ctx context.Context, accountID int64) (string, error) {
	var ciphertext string
	if err := s.db.QueryRowContext(ctx, `SELECT refresh_token_encrypted FROM drive_storage_accounts WHERE id=$1`, accountID).Scan(&ciphertext); err != nil {
		return "", err
	}
	token, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", errors.New("cannot decrypt Drive credential")
	}
	return token, nil
}

// UpdateRefreshToken stores a rotated refresh token.
func (s *DriveStore) UpdateRefreshToken(ctx context.Context, accountID int64, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return errors.New("invalid Drive credential")
	}
	encrypted, err := s.encryptor.Encrypt(refreshToken)
	if err != nil {
		return errors.New("cannot encrypt Drive credential")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET refresh_token_encrypted=$2,updated_at=NOW() WHERE id=$1`, accountID, encrypted)
	return err
}

// RecordQuota stores a successful quota check and clears the last error.
func (s *DriveStore) RecordQuota(ctx context.Context, accountID int64, limit, usage *int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET quota_limit=$2,quota_usage=$3,
        quota_checked_at=NOW(),last_error='',updated_at=NOW() WHERE id=$1`, accountID, limit, usage)
	return err
}

// RecordError marks an account unhealthy so no new files are placed there.
func (s *DriveStore) RecordError(ctx context.Context, accountID int64, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	if message == "" {
		message = "unknown error"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET last_error=$2,updated_at=NOW() WHERE id=$1`, accountID, message)
	return err
}

// AdjustUsage keeps the quota snapshot current between checks.
func (s *DriveStore) AdjustUsage(ctx context.Context, accountID, delta int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET quota_usage=GREATEST(quota_usage+$2,0)
        WHERE id=$1 AND quota_usage IS NOT NULL`, accountID, delta)
	return err
}

func (s *DriveStore) SetAccountEnabled(ctx context.Context, accountID int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET enabled=$2,updated_at=NOW() WHERE id=$1`, accountID, enabled)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return os.ErrNotExist
	}
	return nil
}

// ErrDriveAccountInUse is returned when removing an account that still holds files.
var ErrDriveAccountInUse = errors.New("this storage account still holds files; disable it instead")

// DeleteAccount removes an account that holds no files.
func (s *DriveStore) DeleteAccount(ctx context.Context, accountID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM drive_storage_accounts a WHERE a.id=$1
        AND NOT EXISTS (SELECT 1 FROM drive_nodes n WHERE n.storage_account_id=a.id)`, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Account(ctx, accountID); errors.Is(err, sql.ErrNoRows) {
			return os.ErrNotExist
		}
		return ErrDriveAccountInUse
	}
	return nil
}

func (s *DriveStore) ClearImport(ctx context.Context, accountID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE drive_storage_accounts SET import_user_id=NULL WHERE id=$1`, accountID)
	return err
}

// ---- nodes ----

const nodeColumns = `id,user_id,COALESCE(parent_id,0),name,is_dir,size,mime_type,COALESCE(storage_account_id,0),
    COALESCE(google_file_id,''),state,created_at,updated_at`

func scanNode(row interface{ Scan(...any) error }) (*DriveNode, error) {
	var n DriveNode
	err := row.Scan(&n.ID, &n.UserID, &n.ParentID, &n.Name, &n.IsDir, &n.Size, &n.MimeType, &n.StorageAccountID,
		&n.GoogleFileID, &n.State, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, os.ErrNotExist
	}
	return &n, err
}

func scanNodes(rows *sql.Rows, err error) ([]*DriveNode, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DriveNode{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Child returns the ready entry called name inside parentID.
func (s *DriveStore) Child(ctx context.Context, userID, parentID int64, name string) (*DriveNode, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes
        WHERE user_id=$1 AND COALESCE(parent_id,0)=$2 AND name=$3 AND state='ready'`, userID, parentID, name))
}

// Node returns one of the user's nodes in any state.
func (s *DriveStore) Node(ctx context.Context, userID, id int64) (*DriveNode, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes WHERE user_id=$1 AND id=$2`, userID, id))
}

func (s *DriveStore) Children(ctx context.Context, userID, parentID int64) ([]*DriveNode, error) {
	return scanNodes(s.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes
        WHERE user_id=$1 AND COALESCE(parent_id,0)=$2 AND state='ready' ORDER BY is_dir DESC, name LIMIT 10000`, userID, parentID))
}

func (s *DriveStore) CreateDir(ctx context.Context, userID, parentID int64, name string) (*DriveNode, error) {
	n, err := scanNode(s.db.QueryRowContext(ctx, `INSERT INTO drive_nodes (user_id,parent_id,name,is_dir)
        VALUES ($1,$2,$3,TRUE) RETURNING `+nodeColumns, userID, nullParent(parentID), name))
	if isUniqueViolation(err) {
		return nil, os.ErrExist
	}
	return n, err
}

// CreatePending reserves space for a file that is being uploaded to accountID.
func (s *DriveStore) CreatePending(ctx context.Context, userID, parentID int64, name, mimeType string, size, accountID int64, ttl time.Duration) (*DriveNode, error) {
	// Serialize reservations across processes, then recheck limits inside the
	// same transaction. Selection is advisory; concurrent uploads must not
	// both spend the same remaining user or storage capacity.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(25120261030)"); err != nil {
		return nil, err
	}
	var quota sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT quota_bytes FROM drive_user_quotas WHERE user_id=$1", userID).Scan(&quota)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var used int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM drive_nodes
        WHERE user_id=$1 AND NOT is_dir AND (state='ready' OR pending_expires_at>NOW())`, userID).Scan(&used); err != nil {
		return nil, err
	}
	if quota.Valid && (size > quota.Int64 || used > quota.Int64-size) {
		return nil, ErrDriveQuotaExceeded
	}
	var limit, usage sql.NullInt64
	var enabled bool
	var lastError string
	if err = tx.QueryRowContext(ctx, `SELECT quota_limit,quota_usage,enabled,last_error
        FROM drive_storage_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&limit, &usage, &enabled, &lastError); err != nil {
		return nil, err
	}
	if !enabled || lastError != "" {
		return nil, ErrDriveNoStorage
	}
	var reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM drive_nodes
        WHERE storage_account_id=$1 AND state='pending' AND pending_expires_at>NOW()`, accountID).Scan(&reserved); err != nil {
		return nil, err
	}
	if limit.Valid && (size > limit.Int64 || usage.Int64 > limit.Int64-size || reserved > limit.Int64-size-usage.Int64) {
		return nil, ErrDriveNoStorage
	}
	node, err := scanNode(tx.QueryRowContext(ctx, `INSERT INTO drive_nodes
        (user_id,parent_id,name,is_dir,size,mime_type,storage_account_id,state,pending_expires_at)
        VALUES ($1,$2,$3,FALSE,$4,$5,$6,'pending',NOW()+$7::double precision*INTERVAL '1 second') RETURNING `+nodeColumns,
		userID, nullParent(parentID), name, size, mimeType, accountID, int64(ttl/time.Second)))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return node, nil
}

// CompletePending turns a pending upload into a ready file. A ready file with
// the same name is replaced and returned so the caller can delete its object.
func (s *DriveStore) CompletePending(ctx context.Context, nodeID int64, googleFileID string, size int64) (*DriveNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	pending, err := scanNode(tx.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes WHERE id=$1 AND state='pending' FOR UPDATE`, nodeID))
	if err != nil {
		return nil, err
	}
	var replaced *DriveNode
	existing, err := scanNode(tx.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes
        WHERE user_id=$1 AND COALESCE(parent_id,0)=$2 AND name=$3 AND state='ready' FOR UPDATE`, pending.UserID, pending.ParentID, pending.Name))
	switch {
	case err == nil && existing.IsDir:
		return nil, os.ErrExist
	case err == nil:
		if _, err := tx.ExecContext(ctx, `DELETE FROM drive_nodes WHERE id=$1`, existing.ID); err != nil {
			return nil, err
		}
		replaced = existing
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE drive_nodes SET state='ready',google_file_id=$2,size=$3,
        pending_expires_at=NULL,updated_at=NOW() WHERE id=$1`, nodeID, googleFileID, size); err != nil {
		if isUniqueViolation(err) {
			return nil, os.ErrExist
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE drive_storage_accounts SET quota_usage=quota_usage+$2
        WHERE id=$1 AND quota_usage IS NOT NULL`, pending.StorageAccountID, size); err != nil {
		return nil, err
	}
	return replaced, tx.Commit()
}

// DeletePending drops a reservation that will not be completed.
func (s *DriveStore) DeletePending(ctx context.Context, nodeID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM drive_nodes WHERE id=$1 AND state='pending'`, nodeID)
	return err
}

// ExpiredPending lists reservations whose upload window has passed.
func (s *DriveStore) ExpiredPending(ctx context.Context) ([]*DriveNode, error) {
	return scanNodes(s.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM drive_nodes
        WHERE state='pending' AND pending_expires_at<=NOW() ORDER BY id LIMIT 200`))
}

// IsAncestor reports whether ancestorID is nodeID or one of its ancestors.
func (s *DriveStore) IsAncestor(ctx context.Context, userID, ancestorID, nodeID int64) (bool, error) {
	if nodeID <= 0 {
		return false, nil
	}
	var found bool
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,depth) AS (
            SELECT id,parent_id,0 FROM drive_nodes WHERE user_id=$1 AND id=$3
            UNION ALL SELECT n.id,n.parent_id,up.depth+1 FROM drive_nodes n JOIN up ON n.id=up.parent_id WHERE up.depth<256)
        SELECT EXISTS (SELECT 1 FROM up WHERE id=$2)`, userID, ancestorID, nodeID).Scan(&found)
	return found, err
}

// Move renames and/or reparents a node within the user's tree.
func (s *DriveStore) Move(ctx context.Context, userID, nodeID, newParentID int64, newName string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE drive_nodes SET parent_id=$3,name=$4,updated_at=NOW()
        WHERE user_id=$1 AND id=$2 AND state='ready'`, userID, nodeID, nullParent(newParentID), newName)
	if isUniqueViolation(err) {
		return os.ErrExist
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return os.ErrNotExist
	}
	return nil
}

// SubtreeFiles lists the stored files at or below nodeID.
func (s *DriveStore) SubtreeFiles(ctx context.Context, userID, nodeID int64) ([]*DriveNode, error) {
	return scanNodes(s.db.QueryContext(ctx, `WITH RECURSIVE t(id,depth) AS (
            SELECT id,0 FROM drive_nodes WHERE user_id=$1 AND id=$2
            UNION ALL SELECT n.id,t.depth+1 FROM drive_nodes n JOIN t ON n.parent_id=t.id WHERE t.depth<256)
        SELECT `+nodeColumns+` FROM drive_nodes WHERE id IN (SELECT id FROM t) AND NOT is_dir AND google_file_id IS NOT NULL`, userID, nodeID))
}

// DeleteNode removes a node and, through the foreign key, its descendants.
// Callers delete the stored objects first. Usage snapshots are adjusted.
func (s *DriveStore) DeleteNode(ctx context.Context, userID, nodeID int64, files []*DriveNode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM drive_nodes WHERE user_id=$1 AND id=$2`, userID, nodeID); err != nil {
		return err
	}
	for _, f := range files {
		if _, err := tx.ExecContext(ctx, `UPDATE drive_storage_accounts SET quota_usage=GREATEST(quota_usage-$2,0)
            WHERE id=$1 AND quota_usage IS NOT NULL`, f.StorageAccountID, f.Size); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- usage and quotas ----

func (s *DriveStore) Usage(ctx context.Context, userID int64) (*DriveUsage, error) {
	var u DriveUsage
	var quota sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT
        COALESCE(SUM(size) FILTER (WHERE state='ready' AND NOT is_dir),0),
        COALESCE(SUM(size) FILTER (WHERE state='pending' AND pending_expires_at>NOW()),0),
        COUNT(*) FILTER (WHERE state='ready' AND NOT is_dir),
        COUNT(*) FILTER (WHERE is_dir),
        (SELECT quota_bytes FROM drive_user_quotas WHERE user_id=$1)
        FROM drive_nodes WHERE user_id=$1`, userID).Scan(&u.UsedBytes, &u.ReservedBytes, &u.FileCount, &u.FolderCount, &quota)
	if err != nil {
		return nil, err
	}
	if quota.Valid {
		u.QuotaBytes = &quota.Int64
	}
	return &u, nil
}

// UserUsages lists active users with their usage, largest first.
func (s *DriveStore) UserUsages(ctx context.Context) ([]DriveUserUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.email,
        COALESCE(SUM(n.size) FILTER (WHERE n.state='ready' AND NOT n.is_dir),0),
        COALESCE(SUM(n.size) FILTER (WHERE n.state='pending' AND n.pending_expires_at>NOW()),0),
        COUNT(n.id) FILTER (WHERE n.state='ready' AND NOT n.is_dir),
        COUNT(n.id) FILTER (WHERE n.is_dir),
        q.quota_bytes
        FROM users u LEFT JOIN drive_nodes n ON n.user_id=u.id LEFT JOIN drive_user_quotas q ON q.user_id=u.id
        WHERE u.deleted_at IS NULL GROUP BY u.id,u.email,q.quota_bytes
        ORDER BY 3 DESC, u.id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DriveUserUsage{}
	for rows.Next() {
		var item DriveUserUsage
		var quota sql.NullInt64
		if err := rows.Scan(&item.UserID, &item.Email, &item.UsedBytes, &item.ReservedBytes, &item.FileCount, &item.FolderCount, &quota); err != nil {
			return nil, err
		}
		if quota.Valid {
			item.QuotaBytes = &quota.Int64
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// SetQuota sets a user's limit; nil removes it.
func (s *DriveStore) SetQuota(ctx context.Context, userID int64, quota *int64) error {
	if quota == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM drive_user_quotas WHERE user_id=$1`, userID)
		return err
	}
	if *quota < 0 {
		return os.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO drive_user_quotas (user_id,quota_bytes) VALUES ($1,$2)
        ON CONFLICT (user_id) DO UPDATE SET quota_bytes=EXCLUDED.quota_bytes,updated_at=NOW()`, userID, *quota)
	return err
}

// UserEmail returns the email of an active user.
func (s *DriveStore) UserEmail(ctx context.Context, userID int64) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&email)
	return email, err
}
