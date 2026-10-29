package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DriveConnection contains management metadata only, never OAuth credentials.
type DriveConnection struct {
	UserID       int64     `json:"user_id"`
	Label        string    `json:"label"`
	RootFolderID string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DriveConnectionStore persists a single encrypted Google refresh token per user.
// Callers must authorize the requested user ID before invoking these methods.
type DriveConnectionStore struct {
	db        *sql.DB
	encryptor SecretEncryptor
}

func NewDriveConnectionStore(db *sql.DB, encryptor SecretEncryptor) *DriveConnectionStore {
	return &DriveConnectionStore{db: db, encryptor: encryptor}
}

func (s *DriveConnectionStore) Save(ctx context.Context, userID int64, label, refreshToken, rootFolderID string) error {
	if userID <= 0 || strings.TrimSpace(refreshToken) == "" || len(label) > 256 || strings.TrimSpace(rootFolderID) == "" {
		return errors.New("invalid Drive connection")
	}
	encrypted, err := s.encryptor.Encrypt(refreshToken)
	if err != nil {
		return errors.New("cannot encrypt Drive credential")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO google_drive_connections (user_id,label,refresh_token_encrypted,root_folder_id)
        VALUES ($1,$2,$3,$4) ON CONFLICT (user_id) DO UPDATE SET label=EXCLUDED.label,
        refresh_token_encrypted=EXCLUDED.refresh_token_encrypted,root_folder_id=EXCLUDED.root_folder_id,updated_at=NOW()`, userID, label, encrypted, rootFolderID)
	if err != nil {
		return fmt.Errorf("save Drive connection: %w", err)
	}
	return nil
}

func (s *DriveConnectionStore) Get(ctx context.Context, userID int64) (*DriveConnection, error) {
	var c DriveConnection
	err := s.db.QueryRowContext(ctx, `SELECT user_id,label,COALESCE(root_folder_id,''),created_at,updated_at FROM google_drive_connections WHERE user_id=$1`, userID).Scan(&c.UserID, &c.Label, &c.RootFolderID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RefreshToken is strictly server-side and must never be serialized or logged.
func (s *DriveConnectionStore) RefreshToken(ctx context.Context, userID int64) (string, error) {
	var ciphertext string
	if err := s.db.QueryRowContext(ctx, `SELECT refresh_token_encrypted FROM google_drive_connections WHERE user_id=$1`, userID).Scan(&ciphertext); err != nil {
		return "", err
	}
	token, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", errors.New("cannot decrypt Drive credential")
	}
	return token, nil
}

func (s *DriveConnectionStore) Delete(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM google_drive_connections WHERE user_id=$1`, userID)
	return err
}

// UpdateRefreshToken stores a rotated refresh token for an existing connection.
func (s *DriveConnectionStore) UpdateRefreshToken(ctx context.Context, userID int64, refreshToken string) error {
	if userID <= 0 || strings.TrimSpace(refreshToken) == "" {
		return errors.New("invalid Drive credential")
	}
	encrypted, err := s.encryptor.Encrypt(refreshToken)
	if err != nil {
		return errors.New("cannot encrypt Drive credential")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE google_drive_connections SET refresh_token_encrypted=$2,updated_at=NOW() WHERE user_id=$1`, userID, encrypted)
	return err
}

// DriveUserStatus is the administrator view of one Sub2API user.
type DriveUserStatus struct {
	UserID    int64      `json:"user_id"`
	Email     string     `json:"email"`
	Connected bool       `json:"connected"`
	Label     string     `json:"label"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ListUserConnections lists active users and whether each has a Drive connection.
func (s *DriveConnectionStore) ListUserConnections(ctx context.Context) ([]DriveUserStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.email,c.user_id IS NOT NULL,COALESCE(c.label,''),c.updated_at
        FROM users u LEFT JOIN google_drive_connections c ON c.user_id=u.id
        WHERE u.deleted_at IS NULL ORDER BY u.id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DriveUserStatus{}
	for rows.Next() {
		var item DriveUserStatus
		var updated sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Email, &item.Connected, &item.Label, &updated); err != nil {
			return nil, err
		}
		if updated.Valid {
			item.UpdatedAt = &updated.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UserEmail returns the email of an active user.
func (s *DriveConnectionStore) UserEmail(ctx context.Context, userID int64) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&email)
	return email, err
}
