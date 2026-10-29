package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
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
	ErrDriveDisabled     = errors.New("Google Drive integration is not configured")
	ErrDriveNotConnected = errors.New("this user has no Google Drive connection")
	ErrDriveIsFolder     = errors.New("the path is a folder")
)

const defaultDriveMaxBytes = 512 << 20

// GoogleDriveService connects each Sub2API user to an application folder in
// a Google Drive account and exposes it to the admin UI and WebDAV.
type GoogleDriveService struct {
	store          *DriveConnectionStore
	flow           *googledrive.OAuthFlow
	disabledReason string
	maxBytes       int64

	mu         sync.Mutex
	transports map[int64]*googledrive.TokenTransport
	locks      map[int64]webdav.LockSystem
}

// ProvideGoogleDriveService reads the OAuth client from the environment:
// GOOGLE_DRIVE_CLIENT_ID, GOOGLE_DRIVE_CLIENT_SECRET, GOOGLE_DRIVE_REDIRECT_URI
// and optionally GOOGLE_DRIVE_MAX_BYTES. It stays disabled unless the
// encryption key is explicitly configured, because a generated key would make
// stored refresh tokens unreadable after a restart.
func ProvideGoogleDriveService(store *DriveConnectionStore, cfg *config.Config) *GoogleDriveService {
	s := &GoogleDriveService{store: store, maxBytes: defaultDriveMaxBytes, transports: map[int64]*googledrive.TokenTransport{}, locks: map[int64]webdav.LockSystem{}}
	if v, err := strconv.ParseInt(os.Getenv("GOOGLE_DRIVE_MAX_BYTES"), 10, 64); err == nil && v > 0 {
		s.maxBytes = v
	}
	if cfg == nil || !cfg.Totp.EncryptionKeyConfigured {
		s.disabledReason = "TOTP_ENCRYPTION_KEY must be configured to store Drive credentials"
		return s
	}
	flow, err := googledrive.NewOAuthFlow(googledrive.OAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")),
		RedirectURI:  strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_REDIRECT_URI")),
	}, nil)
	if err != nil {
		s.disabledReason = "Google OAuth client is not configured: " + err.Error()
		return s
	}
	s.flow = flow
	return s
}

// Status reports whether the integration is usable and why not.
func (s *GoogleDriveService) Status() (bool, string) {
	return s.flow != nil, s.disabledReason
}

func (s *GoogleDriveService) MaxBytes() int64 { return s.maxBytes }

func (s *GoogleDriveService) ListUsers(ctx context.Context) ([]DriveUserStatus, error) {
	return s.store.ListUserConnections(ctx)
}

// StartAuthorization returns a Google consent URL bound to actor and user.
func (s *GoogleDriveService) StartAuthorization(ctx context.Context, actorID, userID int64) (string, error) {
	if s.flow == nil {
		return "", ErrDriveDisabled
	}
	if _, err := s.store.UserEmail(ctx, userID); err != nil {
		return "", ErrUserNotFound
	}
	return s.flow.Start(actorID, userID)
}

// CompleteAuthorization exchanges the code, ensures the user's root folder
// exists and stores the encrypted refresh token. It returns the user ID.
func (s *GoogleDriveService) CompleteAuthorization(ctx context.Context, state, code string) (int64, error) {
	if s.flow == nil {
		return 0, ErrDriveDisabled
	}
	_, userID, token, err := s.flow.ExchangeByState(ctx, state, code)
	if err != nil {
		return 0, err
	}
	if token.RefreshToken == "" {
		return 0, errors.New("Google did not return a refresh token; remove the app from the Google account's third-party access and authorize again")
	}
	email, err := s.store.UserEmail(ctx, userID)
	if err != nil {
		return 0, ErrUserNotFound
	}
	client := googledrive.NewClient(&http.Client{Transport: &googledrive.TokenTransport{Flow: s.flow, RefreshToken: token.RefreshToken}, Timeout: 60 * time.Second})
	about, err := client.About(ctx)
	if err != nil {
		return 0, err
	}
	rootID := ""
	if existing, err := s.store.Get(ctx, userID); err == nil && existing.RootFolderID != "" {
		// Reuse the previous application folder when it is still reachable.
		if f, err := client.GetFile(ctx, existing.RootFolderID); err == nil && f.MimeType == googledrive.FolderMimeType {
			rootID = f.ID
		}
	}
	if rootID == "" {
		folder, err := client.CreateFolder(ctx, "Sub2API - "+email, "root")
		if err != nil {
			return 0, err
		}
		rootID = folder.ID
	}
	if err := s.store.Save(ctx, userID, about.User.EmailAddress, token.RefreshToken, rootID); err != nil {
		return 0, err
	}
	s.forget(userID)
	return userID, nil
}

// Disconnect removes the stored credential and revokes it at Google. Files
// already in Drive are kept.
func (s *GoogleDriveService) Disconnect(ctx context.Context, userID int64) error {
	refresh, err := s.store.RefreshToken(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDriveNotConnected
	}
	if err := s.store.Delete(ctx, userID); err != nil {
		return err
	}
	s.forget(userID)
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

func (s *GoogleDriveService) forget(userID int64) {
	s.mu.Lock()
	delete(s.transports, userID)
	s.mu.Unlock()
}

// client returns a Drive client and root folder for a connected user.
func (s *GoogleDriveService) client(ctx context.Context, userID int64) (*googledrive.Client, string, error) {
	if s.flow == nil {
		return nil, "", ErrDriveDisabled
	}
	conn, err := s.store.Get(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrDriveNotConnected
	}
	if err != nil {
		return nil, "", err
	}
	if conn.RootFolderID == "" {
		return nil, "", ErrDriveNotConnected
	}
	s.mu.Lock()
	t := s.transports[userID]
	s.mu.Unlock()
	if t == nil {
		refresh, err := s.store.RefreshToken(ctx, userID)
		if err != nil {
			return nil, "", err
		}
		t = &googledrive.TokenTransport{Flow: s.flow, RefreshToken: refresh, SaveRefreshToken: func(ctx context.Context, v string) error {
			return s.store.UpdateRefreshToken(ctx, userID, v)
		}}
		s.mu.Lock()
		if existing := s.transports[userID]; existing != nil {
			t = existing
		} else {
			s.transports[userID] = t
		}
		s.mu.Unlock()
	}
	// No overall timeout: transfers are bounded by the request context.
	return googledrive.NewClient(&http.Client{Transport: t}), conn.RootFolderID, nil
}

// FileSystem returns the user's Drive folder as a WebDAV filesystem with a
// lock system that persists across requests.
func (s *GoogleDriveService) FileSystem(ctx context.Context, userID int64) (webdav.FileSystem, webdav.LockSystem, error) {
	c, root, err := s.client(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	ls := s.locks[userID]
	if ls == nil {
		ls = webdav.NewMemLS()
		s.locks[userID] = ls
	}
	s.mu.Unlock()
	return googledrive.NewFS(c, root, s.maxBytes), ls, nil
}

// DriveOverview is the admin summary for one connected user.
type DriveOverview struct {
	GoogleAccount string `json:"google_account"`
	Limit         *int64 `json:"limit"`
	Usage         *int64 `json:"usage"`
	UsageInDrive  *int64 `json:"usage_in_drive"`
}

func (s *GoogleDriveService) Overview(ctx context.Context, userID int64) (*DriveOverview, error) {
	c, _, err := s.client(ctx, userID)
	if err != nil {
		return nil, err
	}
	about, err := c.About(ctx)
	if err != nil {
		return nil, err
	}
	return &DriveOverview{GoogleAccount: about.User.EmailAddress, Limit: about.StorageQuota.Limit, Usage: about.StorageQuota.Usage, UsageInDrive: about.StorageQuota.UsageInDrive}, nil
}

// DriveEntry is one item in a folder listing.
type DriveEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func cleanDrivePath(p string) string { return path.Clean("/" + p) }

// List returns the entries of a folder inside the user's root.
func (s *GoogleDriveService) List(ctx context.Context, userID int64, dir string) ([]DriveEntry, error) {
	fsys, _, err := s.FileSystem(ctx, userID)
	if err != nil {
		return nil, err
	}
	dir = cleanDrivePath(dir)
	f, err := fsys.OpenFile(ctx, dir, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	infos, err := f.Readdir(0)
	if err != nil {
		return nil, err
	}
	out := make([]DriveEntry, 0, len(infos))
	for _, fi := range infos {
		out = append(out, DriveEntry{Name: fi.Name(), Path: path.Join(dir, fi.Name()), IsDir: fi.IsDir(), Size: fi.Size(), Modified: fi.ModTime()})
	}
	return out, nil
}

func validDriveName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// StartDirectUpload creates a Drive resumable session for dir/name so the
// client can send the bytes straight to Google instead of through this
// server. An existing file with that name is replaced in place.
func (s *GoogleDriveService) StartDirectUpload(ctx context.Context, userID int64, dir, name, contentType string, size int64, origin string) (string, error) {
	name = strings.TrimSpace(name)
	if !validDriveName(name) || size < 0 {
		return "", os.ErrInvalid
	}
	if size > s.maxBytes {
		return "", googledrive.ErrTooLarge
	}
	c, root, err := s.client(ctx, userID)
	if err != nil {
		return "", err
	}
	dir = cleanDrivePath(dir)
	parent, err := c.ResolvePath(ctx, root, dir)
	if errors.Is(err, googledrive.ErrPathNotFound) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if parent.MimeType != googledrive.FolderMimeType {
		return "", os.ErrNotExist
	}
	req := googledrive.UploadSessionRequest{Name: name, ParentID: parent.ID, ContentType: contentType, Size: size, Origin: origin}
	existing, err := c.ResolvePath(ctx, root, path.Join(dir, name))
	switch {
	case errors.Is(err, googledrive.ErrPathNotFound):
	case err != nil:
		return "", err
	case strings.HasPrefix(existing.MimeType, "application/vnd.google-apps."):
		// Folders and native Docs/Sheets cannot receive raw content.
		return "", ErrDriveIsFolder
	default:
		req.ReplaceID = existing.ID
	}
	return c.CreateUploadSession(ctx, req)
}

// Upload writes content to dir/name, replacing an existing file in place.
func (s *GoogleDriveService) Upload(ctx context.Context, userID int64, dir, name string, content io.Reader) error {
	name = strings.TrimSpace(name)
	if !validDriveName(name) {
		return os.ErrInvalid
	}
	fsys, _, err := s.FileSystem(ctx, userID)
	if err != nil {
		return err
	}
	f, err := fsys.OpenFile(ctx, path.Join(cleanDrivePath(dir), name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Open returns a readable file. The caller must close it.
func (s *GoogleDriveService) Open(ctx context.Context, userID int64, p string) (webdav.File, os.FileInfo, error) {
	fsys, _, err := s.FileSystem(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	f, err := fsys.OpenFile(ctx, cleanDrivePath(p), os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		_ = f.Close()
		if err == nil {
			err = ErrDriveIsFolder
		}
		return nil, nil, err
	}
	return f, fi, nil
}

func (s *GoogleDriveService) Mkdir(ctx context.Context, userID int64, p string) error {
	fsys, _, err := s.FileSystem(ctx, userID)
	if err != nil {
		return err
	}
	return fsys.Mkdir(ctx, cleanDrivePath(p), 0o755)
}

// Delete moves a file or folder to the Drive trash.
func (s *GoogleDriveService) Delete(ctx context.Context, userID int64, p string) error {
	fsys, _, err := s.FileSystem(ctx, userID)
	if err != nil {
		return err
	}
	return fsys.RemoveAll(ctx, cleanDrivePath(p))
}
