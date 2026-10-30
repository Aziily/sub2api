package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googledrive"

	"golang.org/x/net/webdav"
)

var (
	ErrDriveDisabled        = errors.New("cloud drive storage is not configured")
	ErrDriveNoStorage       = errors.New("no storage account has enough free space")
	ErrDriveQuotaExceeded   = errors.New("this file exceeds your storage quota")
	ErrDriveIsFolder        = errors.New("the path is a folder")
	ErrDriveUploadNotFound  = errors.New("the upload has not reached storage yet")
	ErrDriveAccountMismatch = errors.New("the Google account that signed in differs from the storage account being reauthorized")
)

const (
	defaultDriveMaxBytes = 512 << 20
	// Direct uploads hold their reservation for Google's session lifetime.
	driveUploadTTL     = 7 * 24 * time.Hour
	driveNodeProperty  = "sub2api_node"
	driveStorageFolder = "Sub2API cloud drive storage"
	driveMaxDepth      = 64
)

// GoogleDriveService runs the cloud drive (网盘). Administrators connect
// Google accounts as a storage pool; each user works with a virtual tree whose
// files are stored as objects in one pool account. Users never see or choose
// the Google accounts behind their files.
type GoogleDriveService struct {
	store          *DriveStore
	flow           *googledrive.OAuthFlow
	disabledReason string
	redirectURI    string
	encryption     bool
	maxBytes       int64

	mu         sync.Mutex
	transports map[int64]*googledrive.TokenTransport
	locks      map[int64]webdav.LockSystem
	maintainMu sync.Mutex
}

// ProvideGoogleDriveService reads GOOGLE_DRIVE_CLIENT_ID,
// GOOGLE_DRIVE_CLIENT_SECRET, GOOGLE_DRIVE_REDIRECT_URI and optionally
// GOOGLE_DRIVE_MAX_BYTES (the largest file relayed through this server). It
// stays disabled unless the encryption key is explicitly configured, because a
// generated key would make stored refresh tokens unreadable after a restart.
func ProvideGoogleDriveService(store *DriveStore, cfg *config.Config) *GoogleDriveService {
	s := newGoogleDriveService(store, cfg)
	if s.flow != nil {
		go s.maintainLoop()
	}
	return s
}

func newGoogleDriveService(store *DriveStore, cfg *config.Config) *GoogleDriveService {
	s := &GoogleDriveService{store: store, maxBytes: defaultDriveMaxBytes, transports: map[int64]*googledrive.TokenTransport{}, locks: map[int64]webdav.LockSystem{}}
	if v, err := strconv.ParseInt(os.Getenv("GOOGLE_DRIVE_MAX_BYTES"), 10, 64); err == nil && v > 0 {
		s.maxBytes = v
	}
	s.redirectURI = strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_REDIRECT_URI"))
	s.encryption = cfg != nil && cfg.Totp.EncryptionKeyConfigured
	if !s.encryption {
		s.disabledReason = "TOTP_ENCRYPTION_KEY must be configured to store Drive credentials"
		return s
	}
	flow, err := googledrive.NewOAuthFlow(googledrive.OAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")),
		RedirectURI:  s.redirectURI,
	}, nil)
	if err != nil {
		s.disabledReason = "Google OAuth client is not configured: " + err.Error()
		return s
	}
	s.flow = flow
	return s
}

func (s *GoogleDriveService) Enabled() bool   { return s.flow != nil }
func (s *GoogleDriveService) MaxBytes() int64 { return s.maxBytes }

// ---- operations: configuration and storage pool ----

// DriveConfigCheck is one item of the administrator's configuration check.
type DriveConfigCheck struct {
	Key    string `json:"key"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// DrivePoolSummary aggregates every enabled, healthy storage account.
type DrivePoolSummary struct {
	Accounts       int   `json:"accounts"`
	UsableAccounts int   `json:"usable_accounts"`
	CapacityBytes  int64 `json:"capacity_bytes"`
	FreeBytes      int64 `json:"free_bytes"`
	Unlimited      bool  `json:"unlimited"`
	StoredBytes    int64 `json:"stored_bytes"`
	FileCount      int64 `json:"file_count"`
}

// DriveAdminStatus is everything the operations page shows.
type DriveAdminStatus struct {
	Enabled     bool                   `json:"enabled"`
	Reason      string                 `json:"reason"`
	RedirectURI string                 `json:"redirect_uri"`
	MaxBytes    int64                  `json:"max_bytes"`
	Checks      []DriveConfigCheck     `json:"checks"`
	Pool        DrivePoolSummary       `json:"pool"`
	Accounts    []*DriveStorageAccount `json:"accounts"`
	Users       []DriveUserUsage       `json:"users"`
}

func summarize(accounts []*DriveStorageAccount) DrivePoolSummary {
	var p DrivePoolSummary
	p.Accounts = len(accounts)
	for _, a := range accounts {
		p.StoredBytes += a.StoredBytes
		p.FileCount += a.FileCount
		if !a.Usable() {
			continue
		}
		p.UsableAccounts++
		if a.QuotaLimit == nil {
			p.Unlimited = true
			continue
		}
		p.CapacityBytes += *a.QuotaLimit
		p.FreeBytes += a.Free()
	}
	return p
}

// AdminStatus returns the configuration check, the pool and per-user usage.
// requestHost is the host the administrator is using, to check the callback.
func (s *GoogleDriveService) AdminStatus(ctx context.Context, requestHost string) (*DriveAdminStatus, error) {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.store.UserUsages(ctx)
	if err != nil {
		return nil, err
	}
	st := &DriveAdminStatus{Enabled: s.Enabled(), Reason: s.disabledReason, RedirectURI: s.redirectURI, MaxBytes: s.maxBytes, Pool: summarize(accounts), Accounts: accounts, Users: users}
	st.Checks = append(st.Checks, DriveConfigCheck{Key: "encryption", OK: s.encryption})
	st.Checks = append(st.Checks, DriveConfigCheck{Key: "oauth_client", OK: s.flow != nil, Detail: s.disabledReason})
	redirectOK, detail := false, s.redirectURI
	if u, err := url.Parse(s.redirectURI); err == nil && u.Path == "/auth/drive/callback" {
		redirectOK = u.Scheme == "https" && (requestHost == "" || strings.EqualFold(u.Host, requestHost))
	}
	st.Checks = append(st.Checks, DriveConfigCheck{Key: "redirect_uri", OK: redirectOK, Detail: detail})
	st.Checks = append(st.Checks, DriveConfigCheck{Key: "storage", OK: st.Pool.UsableAccounts > 0, Detail: strconv.Itoa(st.Pool.UsableAccounts) + "/" + strconv.Itoa(st.Pool.Accounts)})
	return st, nil
}

// StartAccountAuthorization returns a Google consent URL that adds a storage
// account (accountID 0) or renews the credential of an existing one.
func (s *GoogleDriveService) StartAccountAuthorization(ctx context.Context, actorID, accountID int64) (string, error) {
	if s.flow == nil {
		return "", ErrDriveDisabled
	}
	if accountID > 0 {
		if _, err := s.store.Account(ctx, accountID); err != nil {
			return "", os.ErrNotExist
		}
	}
	return s.flow.Start(actorID, accountID)
}

// CompleteAuthorization exchanges the code and stores the storage account.
func (s *GoogleDriveService) CompleteAuthorization(ctx context.Context, state, code string) (*DriveStorageAccount, error) {
	if s.flow == nil {
		return nil, ErrDriveDisabled
	}
	_, target, token, err := s.flow.ExchangeByState(ctx, state, code)
	if err != nil {
		return nil, err
	}
	if token.RefreshToken == "" {
		return nil, errors.New("Google did not return a refresh token; remove the app from the Google account's third-party access and authorize again")
	}
	client := googledrive.NewClient(&http.Client{Transport: &googledrive.TokenTransport{Flow: s.flow, RefreshToken: token.RefreshToken}, Timeout: 60 * time.Second})
	about, err := client.About(ctx)
	if err != nil {
		return nil, err
	}
	email := strings.TrimSpace(about.User.EmailAddress)
	if email == "" {
		return nil, errors.New("Google did not report the account email")
	}
	var existing *DriveStorageAccount
	if target > 0 {
		existing, err = s.store.Account(ctx, target)
		if err != nil {
			return nil, os.ErrNotExist
		}
		if !strings.EqualFold(existing.GoogleEmail, email) {
			return nil, ErrDriveAccountMismatch
		}
	} else if a, err := s.store.AccountByEmail(ctx, email); err == nil {
		existing = a
	}
	rootID := ""
	if existing != nil {
		// Keep using the storage folder that holds this account's objects.
		if f, err := client.GetFile(ctx, existing.RootFolderID); err == nil && f.MimeType == googledrive.FolderMimeType {
			rootID = f.ID
		} else if existing.FileCount > 0 {
			return nil, errors.New("the storage folder of this account is no longer reachable")
		}
	}
	if rootID == "" {
		folder, err := client.CreateFolder(ctx, driveStorageFolder, "root")
		if err != nil {
			return nil, err
		}
		rootID = folder.ID
	}
	id, err := s.store.SaveAccount(ctx, email, token.RefreshToken, rootID)
	if err != nil {
		return nil, err
	}
	s.forget(id)
	if _, err := s.RefreshQuota(ctx, id); err != nil {
		slog.Warn("drive quota check after authorization failed", "account_id", id, "error", err)
	}
	return s.store.Account(ctx, id)
}

func safeDriveError(err error) string {
	var apiErr *googledrive.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Google Drive did not respond in time"
	}
	return "Google Drive request failed"
}

// RefreshQuota reads the account's quota from Google and records the result.
func (s *GoogleDriveService) RefreshQuota(ctx context.Context, accountID int64) (*DriveStorageAccount, error) {
	c, _, err := s.client(ctx, accountID)
	if err != nil {
		return nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	about, err := c.About(qctx)
	if err != nil {
		_ = s.store.RecordError(ctx, accountID, safeDriveError(err))
		return nil, err
	}
	if err := s.store.RecordQuota(ctx, accountID, about.StorageQuota.Limit, about.StorageQuota.Usage); err != nil {
		return nil, err
	}
	return s.store.Account(ctx, accountID)
}

func (s *GoogleDriveService) SetAccountEnabled(ctx context.Context, accountID int64, enabled bool) error {
	return s.store.SetAccountEnabled(ctx, accountID, enabled)
}

// RemoveAccount deletes an empty storage account and revokes its credential.
func (s *GoogleDriveService) RemoveAccount(ctx context.Context, accountID int64) error {
	refresh, err := s.store.RefreshToken(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return os.ErrNotExist
	}
	if err := s.store.DeleteAccount(ctx, accountID); err != nil {
		return err
	}
	s.forget(accountID)
	if refresh != "" {
		revokeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(revokeCtx, http.MethodPost, "https://oauth2.googleapis.com/revoke", strings.NewReader(url.Values{"token": {refresh}}.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
			}
		}
	}
	return nil
}

// SetUserQuota sets a user's limit in bytes; nil means no per-user limit.
func (s *GoogleDriveService) SetUserQuota(ctx context.Context, userID int64, quota *int64) error {
	if _, err := s.store.UserEmail(ctx, userID); err != nil {
		return ErrUserNotFound
	}
	return s.store.SetQuota(ctx, userID, quota)
}

func (s *GoogleDriveService) forget(accountID int64) {
	s.mu.Lock()
	delete(s.transports, accountID)
	s.mu.Unlock()
}

// client returns an authenticated Drive client for a storage account.
func (s *GoogleDriveService) client(ctx context.Context, accountID int64) (*googledrive.Client, *DriveStorageAccount, error) {
	if s.flow == nil {
		return nil, nil, ErrDriveDisabled
	}
	account, err := s.store.Account(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, os.ErrNotExist
	}
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	t := s.transports[accountID]
	s.mu.Unlock()
	if t == nil {
		refresh, err := s.store.RefreshToken(ctx, accountID)
		if err != nil {
			return nil, nil, err
		}
		t = &googledrive.TokenTransport{Flow: s.flow, RefreshToken: refresh, SaveRefreshToken: func(ctx context.Context, v string) error {
			return s.store.UpdateRefreshToken(ctx, accountID, v)
		}}
		s.mu.Lock()
		if existing := s.transports[accountID]; existing != nil {
			t = existing
		} else {
			s.transports[accountID] = t
		}
		s.mu.Unlock()
	}
	// No overall timeout: transfers are bounded by the request context.
	return googledrive.NewClient(&http.Client{Transport: t}), account, nil
}

// CheckWrite reports whether a relayed write of size bytes, replacing a file
// of replaced bytes, fits the relay limit, the user's quota and the pool.
func (s *GoogleDriveService) CheckWrite(ctx context.Context, userID, size, replaced int64) error {
	if s.flow == nil {
		return ErrDriveDisabled
	}
	if size > s.maxBytes {
		return googledrive.ErrTooLarge
	}
	usage, err := s.store.Usage(ctx, userID)
	if err != nil {
		return err
	}
	if usage.QuotaBytes != nil && usage.UsedBytes-replaced+usage.ReservedBytes+size > *usage.QuotaBytes {
		return ErrDriveQuotaExceeded
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if chooseDriveAccount(accounts, size) == nil {
		return ErrDriveNoStorage
	}
	return nil
}

// pickAccount chooses the usable account with the most free space for size
// bytes, after checking the user's own quota.
func (s *GoogleDriveService) pickAccount(ctx context.Context, userID, size int64) (*DriveStorageAccount, error) {
	if s.flow == nil {
		return nil, ErrDriveDisabled
	}
	usage, err := s.store.Usage(ctx, userID)
	if err != nil {
		return nil, err
	}
	if usage.QuotaBytes != nil && usage.UsedBytes+usage.ReservedBytes+size > *usage.QuotaBytes {
		return nil, ErrDriveQuotaExceeded
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	best := chooseDriveAccount(accounts, size)
	if best == nil {
		return nil, ErrDriveNoStorage
	}
	return best, nil
}

// chooseDriveAccount returns the usable account that can hold size bytes and
// has the most free space (an account without a limit beats any limit), so
// new files spread across the pool instead of filling one account first.
func chooseDriveAccount(accounts []*DriveStorageAccount, size int64) *DriveStorageAccount {
	var best *DriveStorageAccount
	for _, a := range accounts {
		if !a.Usable() {
			continue
		}
		free := a.Free()
		if free >= 0 && free < size {
			continue
		}
		if best == nil || (best.Free() >= 0 && (free < 0 || free > best.Free())) {
			best = a
		}
	}
	return best
}

// ---- users: the virtual tree ----

// DriveEntry is one item in a folder listing.
type DriveEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mime_type"`
	Modified time.Time `json:"modified"`
}

// DriveOverview is what a user sees about their own cloud drive.
type DriveOverview struct {
	Enabled bool `json:"enabled"`
	DriveUsage
	// AvailableBytes is what the user can still store, limited by both their
	// quota and the pool's free space; nil means no known limit.
	AvailableBytes *int64 `json:"available_bytes"`
	MaxRelayBytes  int64  `json:"max_relay_bytes"`
}

func (s *GoogleDriveService) Overview(ctx context.Context, userID int64) (*DriveOverview, error) {
	usage, err := s.store.Usage(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &DriveOverview{Enabled: s.Enabled(), DriveUsage: *usage, MaxRelayBytes: s.maxBytes}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	pool := summarize(accounts)
	var available *int64
	if pool.UsableAccounts == 0 {
		zero := int64(0)
		available = &zero
	} else if !pool.Unlimited {
		v := pool.FreeBytes
		available = &v
	}
	if usage.QuotaBytes != nil {
		left := *usage.QuotaBytes - usage.UsedBytes - usage.ReservedBytes
		if left < 0 {
			left = 0
		}
		if available == nil || left < *available {
			available = &left
		}
	}
	out.AvailableBytes = available
	return out, nil
}

func cleanDrivePath(p string) string { return path.Clean("/" + p) }

func validDriveName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00")
}

// resolve walks the user's tree. The root is a virtual folder with ID 0.
func (s *GoogleDriveService) resolve(ctx context.Context, userID int64, p string) (*DriveNode, error) {
	p = cleanDrivePath(p)
	node := &DriveNode{UserID: userID, Name: "/", IsDir: true, State: "ready"}
	if p == "/" {
		return node, nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) > driveMaxDepth {
		return nil, os.ErrInvalid
	}
	for _, part := range parts {
		if !node.IsDir {
			return nil, os.ErrNotExist
		}
		child, err := s.store.Child(ctx, userID, node.ID, part)
		if err != nil {
			return nil, err
		}
		node = child
	}
	return node, nil
}

// parentOf resolves the folder that should contain p and returns the base name.
func (s *GoogleDriveService) parentOf(ctx context.Context, userID int64, p string) (*DriveNode, string, error) {
	p = cleanDrivePath(p)
	if p == "/" {
		return nil, "", os.ErrInvalid
	}
	dir, base := path.Split(p)
	if !validDriveName(base) {
		return nil, "", os.ErrInvalid
	}
	parent, err := s.resolve(ctx, userID, dir)
	if err != nil {
		return nil, "", err
	}
	if !parent.IsDir {
		return nil, "", os.ErrNotExist
	}
	return parent, base, nil
}

func entryOf(dir string, n *DriveNode) DriveEntry {
	return DriveEntry{Name: n.Name, Path: path.Join(dir, n.Name), IsDir: n.IsDir, Size: n.Size, MimeType: n.MimeType, Modified: n.UpdatedAt}
}

func (s *GoogleDriveService) List(ctx context.Context, userID int64, dir string) ([]DriveEntry, error) {
	dir = cleanDrivePath(dir)
	node, err := s.resolve(ctx, userID, dir)
	if err != nil {
		return nil, err
	}
	if !node.IsDir {
		return nil, os.ErrNotExist
	}
	children, err := s.store.Children(ctx, userID, node.ID)
	if err != nil {
		return nil, err
	}
	out := make([]DriveEntry, 0, len(children))
	for _, n := range children {
		out = append(out, entryOf(dir, n))
	}
	return out, nil
}

func (s *GoogleDriveService) Mkdir(ctx context.Context, userID int64, p string) error {
	parent, base, err := s.parentOf(ctx, userID, p)
	if errors.Is(err, os.ErrInvalid) && cleanDrivePath(p) == "/" {
		return os.ErrExist
	}
	if err != nil {
		return err
	}
	_, err = s.store.CreateDir(ctx, userID, parent.ID, base)
	return err
}

// Delete removes a file or folder and permanently deletes the stored objects.
func (s *GoogleDriveService) Delete(ctx context.Context, userID int64, p string) error {
	if cleanDrivePath(p) == "/" {
		return os.ErrPermission
	}
	node, err := s.resolve(ctx, userID, p)
	if err != nil {
		return err
	}
	files, err := s.store.SubtreeFiles(ctx, userID, node.ID)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := s.deleteObject(ctx, f.StorageAccountID, f.GoogleFileID); err != nil {
			return err
		}
	}
	return s.store.DeleteNode(ctx, userID, node.ID, files)
}

func (s *GoogleDriveService) deleteObject(ctx context.Context, accountID int64, fileID string) error {
	if fileID == "" {
		return nil
	}
	c, _, err := s.client(ctx, accountID)
	if err != nil {
		return err
	}
	return c.DeleteFile(ctx, fileID)
}

// Move renames or moves an entry. Only the tree changes; stored objects stay.
func (s *GoogleDriveService) Move(ctx context.Context, userID int64, from, to string) error {
	from, to = cleanDrivePath(from), cleanDrivePath(to)
	if from == "/" || to == "/" {
		return os.ErrPermission
	}
	node, err := s.resolve(ctx, userID, from)
	if err != nil {
		return err
	}
	parent, base, err := s.parentOf(ctx, userID, to)
	if err != nil {
		return err
	}
	if node.IsDir {
		inside, err := s.store.IsAncestor(ctx, userID, node.ID, parent.ID)
		if err != nil {
			return err
		}
		if inside {
			return os.ErrInvalid
		}
	}
	return s.store.Move(ctx, userID, node.ID, parent.ID, base)
}

// Open returns a readable file. The caller must close it.
func (s *GoogleDriveService) Open(ctx context.Context, userID int64, p string) (webdav.File, os.FileInfo, error) {
	node, err := s.resolve(ctx, userID, p)
	if err != nil {
		return nil, nil, err
	}
	if node.IsDir {
		return nil, nil, ErrDriveIsFolder
	}
	f := s.openRead(ctx, node)
	return f, f.info, nil
}

// DriveUploadTicket lets a client send a file's bytes straight to Google.
type DriveUploadTicket struct {
	UploadID  int64  `json:"upload_id"`
	UploadURL string `json:"upload_url"`
	Path      string `json:"path"`
}

// StartDirectUpload reserves space and opens a resumable session in the pool
// account with the most free space. The client PUTs the bytes to UploadURL and
// then calls CompleteDirectUpload, so file content never passes this server.
func (s *GoogleDriveService) StartDirectUpload(ctx context.Context, userID int64, dir, name, contentType string, size int64, origin string) (*DriveUploadTicket, error) {
	name = strings.TrimSpace(name)
	if !validDriveName(name) || size < 0 || strings.ContainsAny(contentType, "\r\n") {
		return nil, os.ErrInvalid
	}
	target := path.Join(cleanDrivePath(dir), name)
	parent, _, err := s.parentOf(ctx, userID, target)
	if err != nil {
		return nil, err
	}
	if existing, err := s.store.Child(ctx, userID, parent.ID, name); err == nil && existing.IsDir {
		return nil, ErrDriveIsFolder
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	account, err := s.pickAccount(ctx, userID, size)
	if err != nil {
		return nil, err
	}
	node, err := s.store.CreatePending(ctx, userID, parent.ID, name, contentType, size, account.ID, driveUploadTTL)
	if err != nil {
		return nil, err
	}
	c, _, err := s.client(ctx, account.ID)
	if err == nil {
		var uri string
		uri, err = c.CreateUploadSession(ctx, googledrive.UploadSessionRequest{
			Name: objectName(node), ParentID: account.RootFolderID, ContentType: contentType, Size: size, Origin: origin,
			AppProperties: map[string]string{driveNodeProperty: nodeTag(node.ID)},
		})
		if err == nil {
			return &DriveUploadTicket{UploadID: node.ID, UploadURL: uri, Path: target}, nil
		}
	}
	_ = s.store.DeletePending(context.WithoutCancel(ctx), node.ID)
	return nil, err
}

func nodeTag(id int64) string { return "n" + strconv.FormatInt(id, 10) }

// objectName labels stored objects so an administrator browsing the storage
// account can tell them apart. The tree, not this name, is authoritative.
func objectName(n *DriveNode) string {
	return strconv.FormatInt(n.UserID, 10) + "-" + strconv.FormatInt(n.ID, 10) + "-" + n.Name
}

// CompleteDirectUpload records a direct upload once Google holds the object.
// The object is looked up by its private tag rather than trusted from the client.
func (s *GoogleDriveService) CompleteDirectUpload(ctx context.Context, userID, uploadID int64) (*DriveEntry, error) {
	node, err := s.store.Node(ctx, userID, uploadID)
	if err != nil || node.State != "pending" {
		return nil, ErrDriveUploadNotFound
	}
	if err := s.finishPending(ctx, node); err != nil {
		return nil, err
	}
	done, err := s.store.Node(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	dir, _ := s.pathOf(ctx, userID, done.ParentID)
	e := entryOf(dir, done)
	return &e, nil
}

func (s *GoogleDriveService) finishPending(ctx context.Context, node *DriveNode) error {
	c, account, err := s.client(ctx, node.StorageAccountID)
	if err != nil {
		return err
	}
	files, err := c.FindByAppProperty(ctx, account.RootFolderID, driveNodeProperty, nodeTag(node.ID))
	if err != nil {
		return err
	}
	if len(files) == 0 || files[0].Size == nil {
		return ErrDriveUploadNotFound
	}
	return s.complete(ctx, node, files[0].ID, *files[0].Size)
}

func (s *GoogleDriveService) complete(ctx context.Context, node *DriveNode, fileID string, size int64) error {
	replaced, err := s.store.CompletePending(ctx, node.ID, fileID, size)
	if err != nil {
		return err
	}
	if replaced != nil {
		bg := context.WithoutCancel(ctx)
		if err := s.deleteObject(bg, replaced.StorageAccountID, replaced.GoogleFileID); err != nil {
			slog.Warn("drive: could not delete replaced object", "account_id", replaced.StorageAccountID, "error", safeDriveError(err))
		} else {
			_ = s.store.AdjustUsage(bg, replaced.StorageAccountID, -replaced.Size)
		}
	}
	return nil
}

// CancelDirectUpload drops a reservation and any object already stored for it.
func (s *GoogleDriveService) CancelDirectUpload(ctx context.Context, userID, uploadID int64) error {
	node, err := s.store.Node(ctx, userID, uploadID)
	if err != nil || node.State != "pending" {
		return ErrDriveUploadNotFound
	}
	s.discardPending(ctx, node)
	return nil
}

func (s *GoogleDriveService) discardPending(ctx context.Context, node *DriveNode) {
	if c, account, err := s.client(ctx, node.StorageAccountID); err == nil {
		if files, err := c.FindByAppProperty(ctx, account.RootFolderID, driveNodeProperty, nodeTag(node.ID)); err == nil {
			for _, f := range files {
				_ = c.DeleteFile(ctx, f.ID)
			}
		}
	}
	_ = s.store.DeletePending(ctx, node.ID)
}

// pathOf rebuilds the path of a folder for display.
func (s *GoogleDriveService) pathOf(ctx context.Context, userID, folderID int64) (string, error) {
	parts := []string{}
	for id := folderID; id > 0 && len(parts) < driveMaxDepth; {
		n, err := s.store.Node(ctx, userID, id)
		if err != nil {
			return "/", err
		}
		parts = append([]string{n.Name}, parts...)
		id = n.ParentID
	}
	return "/" + strings.Join(parts, "/"), nil
}

// Upload stores content relayed through this server (form uploads and
// WebDAV). size must be exact; content must provide exactly size bytes.
func (s *GoogleDriveService) Upload(ctx context.Context, userID int64, p, contentType string, size int64, content io.Reader) error {
	if size < 0 {
		return os.ErrInvalid
	}
	if size > s.maxBytes {
		return googledrive.ErrTooLarge
	}
	parent, name, err := s.parentOf(ctx, userID, p)
	if err != nil {
		return err
	}
	if existing, err := s.store.Child(ctx, userID, parent.ID, name); err == nil && existing.IsDir {
		return ErrDriveIsFolder
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	account, err := s.pickAccount(ctx, userID, size)
	if err != nil {
		return err
	}
	node, err := s.store.CreatePending(ctx, userID, parent.ID, name, contentType, size, account.ID, time.Hour)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		s.discardPending(context.WithoutCancel(ctx), node)
		return err
	}
	c, _, err := s.client(ctx, account.ID)
	if err != nil {
		return fail(err)
	}
	uri, err := c.CreateUploadSession(ctx, googledrive.UploadSessionRequest{
		Name: objectName(node), ParentID: account.RootFolderID, ContentType: contentType, Size: size,
		AppProperties: map[string]string{driveNodeProperty: nodeTag(node.ID)},
	})
	if err != nil {
		return fail(err)
	}
	file, err := c.UploadToSession(ctx, uri, size, content)
	if err != nil {
		return fail(err)
	}
	stored := size
	if file.Size != nil {
		stored = *file.Size
	}
	if err := s.complete(ctx, node, file.ID, stored); err != nil {
		return fail(err)
	}
	return nil
}

// FileSystem returns the user's tree as a WebDAV filesystem with a lock
// system that persists across requests.
func (s *GoogleDriveService) FileSystem(ctx context.Context, userID int64) (webdav.FileSystem, webdav.LockSystem, error) {
	if s.flow == nil {
		return nil, nil, ErrDriveDisabled
	}
	s.mu.Lock()
	ls := s.locks[userID]
	if ls == nil {
		ls = webdav.NewMemLS()
		s.locks[userID] = ls
	}
	s.mu.Unlock()
	return &driveFS{s: s, userID: userID}, ls, nil
}

// ---- maintenance ----

func (s *GoogleDriveService) maintainLoop() {
	time.Sleep(15 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		s.Maintain(ctx)
		cancel()
		time.Sleep(30 * time.Minute)
	}
}

// Maintain refreshes quotas, imports legacy folders and settles expired
// upload reservations: uploads that reached Google are recorded, others dropped.
func (s *GoogleDriveService) Maintain(ctx context.Context) {
	if s.flow == nil || !s.maintainMu.TryLock() {
		return
	}
	defer s.maintainMu.Unlock()
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		slog.Warn("drive maintenance: list accounts", "error", err)
		return
	}
	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		if a.ImportUserID > 0 {
			if err := s.importLegacy(ctx, a); err != nil {
				slog.Warn("drive maintenance: import legacy folder", "account_id", a.ID, "error", safeDriveError(err))
			}
		}
		if _, err := s.RefreshQuota(ctx, a.ID); err != nil {
			slog.Warn("drive maintenance: quota check failed", "account_id", a.ID, "error", safeDriveError(err))
		}
	}
	expired, err := s.store.ExpiredPending(ctx)
	if err != nil {
		return
	}
	for _, n := range expired {
		if err := s.finishPending(ctx, n); err != nil {
			s.discardPending(ctx, n)
		}
	}
}

// importLegacy adds the files of a former per-user folder to that user's tree.
func (s *GoogleDriveService) importLegacy(ctx context.Context, a *DriveStorageAccount) error {
	c, _, err := s.client(ctx, a.ID)
	if err != nil {
		return err
	}
	if err := s.importFolder(ctx, c, a, a.RootFolderID, 0, 0); err != nil {
		return err
	}
	return s.store.ClearImport(ctx, a.ID)
}

func (s *GoogleDriveService) importFolder(ctx context.Context, c *googledrive.Client, a *DriveStorageAccount, folderID string, parentID int64, depth int) error {
	if depth > driveMaxDepth {
		return nil
	}
	token := ""
	for pages := 0; pages < 1000; pages++ {
		page, err := c.ListChildren(ctx, folderID, token, 100)
		if err != nil {
			return err
		}
		for _, f := range page.Files {
			if f.AppProperties[driveNodeProperty] != "" || !validDriveName(f.Name) {
				continue // already a pool object, or not addressable
			}
			name := s.freeName(ctx, a.ImportUserID, parentID, f.Name)
			if f.MimeType == googledrive.FolderMimeType {
				dir, err := s.store.CreateDir(ctx, a.ImportUserID, parentID, name)
				if err != nil {
					return err
				}
				if err := s.importFolder(ctx, c, a, f.ID, dir.ID, depth+1); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(f.MimeType, googledrive.NativeDocPrefix) || f.Size == nil {
				continue
			}
			node, err := s.store.CreatePending(ctx, a.ImportUserID, parentID, name, f.MimeType, *f.Size, a.ID, time.Hour)
			if err != nil {
				return err
			}
			if _, err := s.store.CompletePending(ctx, node.ID, f.ID, *f.Size); err != nil {
				return err
			}
		}
		if page.NextPageToken == "" {
			return nil
		}
		token = page.NextPageToken
	}
	return nil
}

// freeName returns name, or "name (n)" if the folder already has that name.
func (s *GoogleDriveService) freeName(ctx context.Context, userID, parentID int64, name string) string {
	candidate := name
	for i := 2; i < 1000; i++ {
		if _, err := s.store.Child(ctx, userID, parentID, candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		ext := path.Ext(name)
		candidate = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(i) + ")" + ext
	}
	return candidate
}
