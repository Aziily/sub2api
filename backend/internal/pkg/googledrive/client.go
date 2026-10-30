// Package googledrive contains the minimal Drive API client used by the Drive
// integration. Authorization and credential storage belong to the caller.
package googledrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://www.googleapis.com/drive/v3/"

// FolderMimeType marks Drive folders; NativeDocPrefix marks Docs, Sheets and
// other Google-native files, which have no raw content to download.
const (
	FolderMimeType  = "application/vnd.google-apps.folder"
	NativeDocPrefix = "application/vnd.google-apps."
)

// ErrTooLarge is returned when content exceeds a configured size limit.
var ErrTooLarge = errors.New("file exceeds the configured size limit")

const fileFields = "id,name,mimeType,size,modifiedTime,parents,appProperties"

type Client struct {
	httpClient *http.Client
	base       string
}

// NewClient uses a token source's authenticated transport. Never log tokens.
func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: client, base: apiBase}
}

type StorageQuota struct {
	Limit             *int64 `json:"limit,string"`
	Usage             *int64 `json:"usage,string"`
	UsageInDrive      *int64 `json:"usageInDrive,string"`
	UsageInDriveTrash *int64 `json:"usageInDriveTrash,string"`
}
type About struct {
	StorageQuota StorageQuota `json:"storageQuota"`
	User         struct {
		EmailAddress string `json:"emailAddress"`
		DisplayName  string `json:"displayName"`
	} `json:"user"`
}
type File struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	MimeType      string            `json:"mimeType"`
	Size          *int64            `json:"size,string"`
	ModifiedTime  string            `json:"modifiedTime"`
	Parents       []string          `json:"parents,omitempty"`
	AppProperties map[string]string `json:"appProperties,omitempty"`
}
type FilePage struct {
	Files         []File `json:"files"`
	NextPageToken string `json:"nextPageToken"`
}

// APIError intentionally omits the response body, which can contain user data.
type APIError struct{ StatusCode int }

func (e *APIError) Error() string {
	return fmt.Sprintf("Google Drive API returned HTTP %d", e.StatusCode)
}

func (c *Client) get(ctx context.Context, path string, params url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return &APIError{StatusCode: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst); err != nil {
		return fmt.Errorf("invalid Google Drive response: %w", err)
	}
	return nil
}

func (c *Client) About(ctx context.Context) (*About, error) {
	var result About
	err := c.get(ctx, "about", url.Values{"fields": {"storageQuota,user(emailAddress,displayName)"}}, &result)
	return &result, err
}

// ListFiles returns only files visible under the caller's granted scopes.
func (c *Client) ListFiles(ctx context.Context, pageToken string, pageSize int) (*FilePage, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100")
	}
	q := url.Values{"fields": {"nextPageToken,files(id,name,mimeType,size,modifiedTime)"}, "pageSize": {strconv.Itoa(pageSize)}, "q": {"trashed = false"}}
	if strings.TrimSpace(pageToken) != "" {
		q.Set("pageToken", pageToken)
	}
	var result FilePage
	err := c.get(ctx, "files", q, &result)
	return &result, err
}

// ListChildren returns a page of non-trashed files in a single Drive folder.
func (c *Client) ListChildren(ctx context.Context, parentID, pageToken string, pageSize int) (*FilePage, error) {
	if !validID(parentID) || pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("invalid folder ID or page size")
	}
	q := url.Values{"fields": {"nextPageToken,files(id,name,mimeType,size,modifiedTime,parents)"}, "pageSize": {strconv.Itoa(pageSize)}, "q": {"trashed = false and '" + parentID + "' in parents"}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	var result FilePage
	err := c.get(ctx, "files", q, &result)
	return &result, err
}

// GetFile returns metadata for a file accessible under the granted scope.
func (c *Client) GetFile(ctx context.Context, fileID string) (*File, error) {
	if !validID(fileID) {
		return nil, fmt.Errorf("invalid file ID")
	}
	var file File
	err := c.get(ctx, "files/"+url.PathEscape(fileID), url.Values{"fields": {fileFields}}, &file)
	return &file, err
}

// FindByAppProperty returns the non-trashed files inside parentID whose
// private application property key equals value. Values are restricted to
// the ID alphabet so they can be embedded in the query safely.
func (c *Client) FindByAppProperty(ctx context.Context, parentID, key, value string) ([]File, error) {
	if !validID(parentID) || !validID(key) || !validID(value) {
		return nil, fmt.Errorf("invalid search")
	}
	q := url.Values{
		"fields":   {"files(" + fileFields + ")"},
		"pageSize": {"10"},
		"q":        {"trashed = false and '" + parentID + "' in parents and appProperties has { key='" + key + "' and value='" + value + "' }"},
	}
	var result FilePage
	if err := c.get(ctx, "files", q, &result); err != nil {
		return nil, err
	}
	return result.Files, nil
}

// DeleteFile permanently deletes a file so its space is released at once.
// The pool's database is the source of truth, so Drive's trash is skipped.
// A file that is already gone counts as deleted.
func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	if !validID(fileID) {
		return fmt.Errorf("invalid file ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"files/"+url.PathEscape(fileID), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return &APIError{StatusCode: resp.StatusCode}
	}
	return nil
}

// DownloadRange streams the file's raw contents starting at offset. The
// caller must close the body.
func (c *Client) DownloadRange(ctx context.Context, fileID string, offset int64) (io.ReadCloser, error) {
	if !validID(fileID) || offset < 0 {
		return nil, fmt.Errorf("invalid download request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"files/"+url.PathEscape(fileID)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive download failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	if offset > 0 && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, errors.New("Google Drive ignored the range request")
	}
	return resp.Body, nil
}

// ReplaceContent updates the contents of an existing non-native Drive file.
func (c *Client) ReplaceContent(ctx context.Context, fileID, contentType string, content io.Reader) (*File, error) {
	if !validID(fileID) || content == nil || strings.ContainsAny(contentType, "\r\n") {
		return nil, fmt.Errorf("invalid file or content")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	endpoint := strings.TrimSuffix(c.base, "/drive/v3/") + "/upload/drive/v3/files/" + url.PathEscape(fileID) + "?uploadType=media&fields=id,name,mimeType,size,modifiedTime,parents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, content)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var file File
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return nil, errors.New("invalid Google Drive response")
	}
	return &file, nil
}

// Drive IDs are opaque ASCII identifiers. Restrict them before placing one
// in either a URL path or a Drive query expression.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// CreateFolder creates an application-owned folder in the specified parent.
func (c *Client) CreateFolder(ctx context.Context, name, parentID string) (*File, error) {
	if strings.TrimSpace(name) == "" || !validID(parentID) {
		return nil, fmt.Errorf("invalid folder name or parent")
	}
	data, err := json.Marshal(map[string]any{"name": name, "parents": []string{parentID}, "mimeType": "application/vnd.google-apps.folder"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"files?fields=id,name,mimeType,parents,modifiedTime", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var file File
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return nil, errors.New("invalid Google Drive response")
	}
	return &file, nil
}

// Trash moves a file to Drive's recoverable trash.
func (c *Client) Trash(ctx context.Context, fileID string) error {
	if !validID(fileID) {
		return fmt.Errorf("invalid file ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.base+"files/"+url.PathEscape(fileID), strings.NewReader(`{"trashed":true}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &APIError{StatusCode: resp.StatusCode}
	}
	return nil
}

// Download returns the file's raw contents. The caller must close the body.
// Native Google documents require an export operation and are not supported here.
func (c *Client) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if !validID(fileID) {
		return nil, fmt.Errorf("invalid file ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"files/"+url.PathEscape(fileID)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive download failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	return resp.Body, nil
}

// Upload creates a file using Drive's multipart upload. Content is streamed;
// callers must enforce their own size limit and cancel the context on failure.
func (c *Client) Upload(ctx context.Context, name, parentID, contentType string, content io.Reader) (*File, error) {
	if strings.TrimSpace(name) == "" || content == nil {
		return nil, fmt.Errorf("file name and content are required")
	}
	if strings.ContainsAny(contentType, "\r\n") {
		return nil, fmt.Errorf("invalid content type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata := map[string]any{"name": name}
	if parentID != "" {
		metadata["parents"] = []string{parentID}
	}
	body, writer := io.Pipe()
	defer body.Close()
	multi := multipart.NewWriter(writer)
	go func() {
		defer writer.Close()
		h := make(textproto.MIMEHeader)
		h.Set("Content-Type", "application/json; charset=UTF-8")
		part, err := multi.CreatePart(h)
		if err == nil {
			err = json.NewEncoder(part).Encode(metadata)
		}
		if err == nil {
			h = make(textproto.MIMEHeader)
			h.Set("Content-Type", contentType)
			part, err = multi.CreatePart(h)
		}
		if err == nil {
			_, err = io.Copy(part, content)
		}
		if err == nil {
			err = multi.Close()
		}
		if err != nil {
			_ = writer.CloseWithError(err)
		}
	}()
	uploadBase := strings.TrimSuffix(c.base, "/drive/v3/") + "/upload/drive/v3/files?uploadType=multipart&fields=id,name,mimeType,size,modifiedTime"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadBase, body)
	if err != nil {
		body.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+multi.Boundary())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		body.Close()
		return nil, fmt.Errorf("Google Drive upload failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var file File
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return nil, fmt.Errorf("invalid Google Drive response: %w", err)
	}
	return &file, nil
}

// Move renames a file and/or moves it from oldParentID to newParentID in a
// single request. Pass equal parent IDs to rename in place.
func (c *Client) Move(ctx context.Context, fileID, newName, oldParentID, newParentID string) (*File, error) {
	if !validID(fileID) || !validID(oldParentID) || !validID(newParentID) || strings.TrimSpace(newName) == "" {
		return nil, fmt.Errorf("invalid move request")
	}
	q := url.Values{"fields": {"id,name,mimeType,size,modifiedTime,parents"}}
	if oldParentID != newParentID {
		q.Set("addParents", newParentID)
		q.Set("removeParents", oldParentID)
	}
	data, err := json.Marshal(map[string]string{"name": newName})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.base+"files/"+url.PathEscape(fileID)+"?"+q.Encode(), strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var file File
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return nil, errors.New("invalid Google Drive response")
	}
	return &file, nil
}
