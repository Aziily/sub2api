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
	UserID    int64     `json:"user_id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

func (s *DriveConnectionStore) Save(ctx context.Context, userID int64, label, refreshToken string) error {
	if userID <= 0 || strings.TrimSpace(refreshToken) == "" || len(label) > 256 {
		return errors.New("invalid Drive connection")
	}
	encrypted, err := s.encryptor.Encrypt(refreshToken)
	if err != nil {
		return errors.New("cannot encrypt Drive credential")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO google_drive_connections (user_id,label,refresh_token_encrypted)
        VALUES ($1,$2,$3) ON CONFLICT (user_id) DO UPDATE SET label=EXCLUDED.label,
        refresh_token_encrypted=EXCLUDED.refresh_token_encrypted,updated_at=NOW()`, userID, label, encrypted)
	if err != nil {
		return fmt.Errorf("save Drive connection: %w", err)
	}
	return nil
}

func (s *DriveConnectionStore) Get(ctx context.Context, userID int64) (*DriveConnection, error) {
	var c DriveConnection
	err := s.db.QueryRowContext(ctx, `SELECT user_id,label,created_at,updated_at FROM google_drive_connections WHERE user_id=$1`, userID).Scan(&c.UserID, &c.Label, &c.CreatedAt, &c.UpdatedAt)
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
