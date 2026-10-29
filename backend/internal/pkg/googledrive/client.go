// Package googledrive contains the minimal Drive API client used by the Drive
// integration. Authorization and credential storage belong to the caller.
package googledrive

import (
	"context"
	"encoding/json"
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
}
type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	Size         *int64 `json:"size,string"`
	ModifiedTime string `json:"modifiedTime"`
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
	err := c.get(ctx, "about", url.Values{"fields": {"storageQuota"}}, &result)
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

// Download returns the file's raw contents. The caller must close the body.
// Native Google documents require an export operation and are not supported here.
func (c *Client) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if fileID == "" || strings.ContainsAny(fileID, "/\\?#") {
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
