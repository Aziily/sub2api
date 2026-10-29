package service

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type driveTestEncryptor struct{}

func (driveTestEncryptor) Encrypt(s string) (string, error) { return "encrypted:" + s, nil }
func (driveTestEncryptor) Decrypt(s string) (string, error) {
	if s != "encrypted:refresh" {
		return "", errors.New("bad ciphertext")
	}
	return "refresh", nil
}
func TestDriveConnectionStoreScopesAndEncryptsCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewDriveConnectionStore(db, driveTestEncryptor{})
	mock.ExpectExec("INSERT INTO google_drive_connections").WithArgs(int64(42), "Murat", "encrypted:refresh").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.Save(context.Background(), 42, "Murat", "refresh"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT refresh_token_encrypted FROM google_drive_connections WHERE user_id=\\$1").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"refresh_token_encrypted"}).AddRow("encrypted:refresh"))
	token, err := store.RefreshToken(context.Background(), 42)
	if err != nil || token != "refresh" {
		t.Fatal("failed to read credential")
	}
	mock.ExpectQuery("SELECT refresh_token_encrypted FROM google_drive_connections WHERE user_id=\\$1").WithArgs(int64(43)).WillReturnError(errors.New("not found"))
	if _, err = store.RefreshToken(context.Background(), 43); err == nil {
		t.Fatal("different user obtained credential")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
