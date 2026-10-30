package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func driveAccount(id int64, limit, usage *int64, reserved int64, enabled bool, lastErr string) *DriveStorageAccount {
	checked := time.Now()
	return &DriveStorageAccount{ID: id, Enabled: enabled, QuotaLimit: limit, QuotaUsage: usage, ReservedBytes: reserved, LastError: lastErr, QuotaCheckedAt: &checked}
}

func gb(n int64) *int64 { v := n << 30; return &v }

func TestChooseDriveAccountSpreadsByFreeSpaceIncludingReservations(t *testing.T) {
	a := driveAccount(1, gb(15), gb(5), 0, true, "")     // 10 GB free
	b := driveAccount(2, gb(15), gb(1), 6<<30, true, "") // 8 GB free after in-flight uploads
	require.Equal(t, int64(1), chooseDriveAccount([]*DriveStorageAccount{b, a}, 1<<30).ID)
	// Only b's reservations keep it from fitting 9 GB; a still fits.
	require.Equal(t, int64(1), chooseDriveAccount([]*DriveStorageAccount{a, b}, 9<<30).ID)
	require.Nil(t, chooseDriveAccount([]*DriveStorageAccount{a, b}, 11<<30))
}

func TestChooseDriveAccountSkipsUnusableAndPrefersUnlimited(t *testing.T) {
	disabled := driveAccount(1, nil, nil, 0, false, "")
	broken := driveAccount(2, nil, nil, 0, true, "invalid_grant")
	unchecked := driveAccount(3, gb(100), gb(0), 0, true, "")
	unchecked.QuotaCheckedAt = nil
	limited := driveAccount(4, gb(100), gb(0), 0, true, "")
	unlimited := driveAccount(5, nil, gb(3), 0, true, "")
	pool := []*DriveStorageAccount{disabled, broken, unchecked, limited, unlimited}
	require.Equal(t, int64(5), chooseDriveAccount(pool, 1<<30).ID)

	sum := summarize(pool)
	require.Equal(t, 5, sum.Accounts)
	require.Equal(t, 2, sum.UsableAccounts)
	require.True(t, sum.Unlimited)
	require.Equal(t, int64(100<<30), sum.CapacityBytes)
}
