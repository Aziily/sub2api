// Package googledrive contains the minimal Drive API client used by the Drive
// integration. Authorization and credential storage belong to the caller.
package googledrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
