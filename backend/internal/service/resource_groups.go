package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

var (
	ErrResourceGroupInput    = errors.New("invalid resource group or account selection")
	ErrResourceGroupMissing  = errors.New("resource group not found")
	ErrResourceGroupConflict = errors.New("group changed or name already exists; refresh and retry")
)

type ResourceGroup struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     int64   `json:"version"`
	Archived    bool    `json:"archived"`
	AccountIDs  []int64 `json:"account_ids"`
}
type ResourceGroupAccount struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Enabled bool   `json:"enabled"`
}
type ResourceGroupList struct {
	Groups   []ResourceGroup        `json:"groups"`
	Accounts []ResourceGroupAccount `json:"accounts"`
}
type ResourceGroupInput struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     int64   `json:"version"`
	Archived    bool    `json:"archived"`
	AccountIDs  []int64 `json:"account_ids"`
}
type ResourceGroupService struct{ db *sql.DB }

func (s *GoogleDriveService) ResourceGroups() *ResourceGroupService {
	return &ResourceGroupService{db: s.store.db}
}

// SQL identifiers are selected only from this fixed allowlist.
func resourceGroupTables(kind string) (table, email, member string, err error) {
	switch kind {
	case "storage":
		return "drive_storage_accounts", "google_email", "storage_account_id", nil
	case "compute":
		return "colab_accounts", "email", "compute_account_id", nil
	default:
		return "", "", "", ErrResourceGroupInput
	}
}
func validateResourceGroup(in *ResourceGroupInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Description) > 1000 || len(in.AccountIDs) > 1000 {
		return ErrResourceGroupInput
	}
	seen := map[int64]bool{}
	for _, id := range in.AccountIDs {
		if id <= 0 || seen[id] {
			return ErrResourceGroupInput
		}
		seen[id] = true
	}
	return nil
}
func (s *ResourceGroupService) List(ctx context.Context, kind string) (*ResourceGroupList, error) {
	table, email, member, err := resourceGroupTables(kind)
	if err != nil {
		return nil, err
	}
	out := &ResourceGroupList{Groups: []ResourceGroup{}, Accounts: []ResourceGroupAccount{}}
	rows, err := s.db.QueryContext(ctx, `SELECT g.id,g.kind,g.name,g.description,g.version,g.archived_at IS NOT NULL,
 COALESCE(array_agg(m.`+member+` ORDER BY m.`+member+`) FILTER (WHERE m.`+member+` IS NOT NULL),'{}')
 FROM resource_groups g LEFT JOIN resource_group_members m ON m.group_id=g.id AND m.kind=g.kind
 WHERE g.kind=$1 GROUP BY g.id ORDER BY g.archived_at NULLS FIRST,lower(g.name),g.id`, kind)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g ResourceGroup
		if err = rows.Scan(&g.ID, &g.Kind, &g.Name, &g.Description, &g.Version, &g.Archived, pq.Array(&g.AccountIDs)); err != nil {
			rows.Close()
			return nil, err
		}
		out.Groups = append(out.Groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,`+email+`,enabled FROM `+table+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ResourceGroupAccount
		if err = rows.Scan(&a.ID, &a.Email, &a.Enabled); err != nil {
			return nil, err
		}
		out.Accounts = append(out.Accounts, a)
	}
	return out, rows.Err()
}
func groupWriteError(err error) error {
	if isUniqueViolation(err) {
		return ErrResourceGroupConflict
	}
	return err
}
func (s *ResourceGroupService) Save(ctx context.Context, kind string, id int64, in ResourceGroupInput) (*ResourceGroup, error) {
	table, _, member, err := resourceGroupTables(kind)
	if err != nil {
		return nil, err
	}
	if err = validateResourceGroup(&in); err != nil {
		return nil, err
	}
	if id < 0 || (id > 0 && in.Version <= 0) || (id == 0 && in.Archived) {
		return nil, ErrResourceGroupInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var version int64
	if id > 0 {
		err = tx.QueryRowContext(ctx, `SELECT version FROM resource_groups WHERE id=$1 AND kind=$2 FOR UPDATE`, id, kind).Scan(&version)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrResourceGroupMissing
		}
		if err != nil {
			return nil, err
		}
		if version != in.Version {
			return nil, ErrResourceGroupConflict
		}
	}
	// Lock members until commit so deleting an account cannot leave a partial edit.
	if len(in.AccountIDs) > 0 {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE id=ANY($1) FOR SHARE`, pq.Array(in.AccountIDs))
		if e != nil {
			return nil, e
		}
		n := 0
		for rows.Next() {
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if n != len(in.AccountIDs) {
			return nil, ErrResourceGroupInput
		}
	}
	if id == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO resource_groups(kind,name,description) VALUES($1,$2,$3) RETURNING id,version`, kind, in.Name, in.Description).Scan(&id, &version)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE resource_groups SET name=$3,description=$4,archived_at=CASE WHEN $5 THEN COALESCE(archived_at,NOW()) ELSE NULL END,version=version+1,updated_at=NOW() WHERE id=$1 AND kind=$2 RETURNING version`, id, kind, in.Name, in.Description, in.Archived).Scan(&version)
	}
	if err != nil {
		return nil, groupWriteError(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM resource_group_members WHERE group_id=$1 AND kind=$2`, id, kind); err != nil {
		return nil, err
	}
	for _, accountID := range in.AccountIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_group_members(group_id,kind,`+member+`) VALUES($1,$2,$3)`, id, kind, accountID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, groupWriteError(err)
	}
	return &ResourceGroup{ID: id, Kind: kind, Name: in.Name, Description: in.Description, Version: version, Archived: in.Archived, AccountIDs: in.AccountIDs}, nil
}
