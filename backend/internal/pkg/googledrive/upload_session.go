package googledrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// UploadSessionRequest describes one file to send directly to Drive.
type UploadSessionRequest struct {
	Name        string
	ParentID    string // folder for a new file
	ReplaceID   string // existing file whose contents are replaced; overrides ParentID
	ContentType string
	Size        int64
	// Origin is the browser origin that will PUT the bytes. Google allows
	// cross-origin requests to the session URI only from this origin.
	Origin string
}

// CreateUploadSession starts a Drive resumable upload and returns its session
// URI. Whoever holds the URI can upload this single file without an OAuth
// token, from any network, until the session expires (about a week), so it
// must be returned only to the authenticated requester and never logged.
func (c *Client) CreateUploadSession(ctx context.Context, r UploadSessionRequest) (string, error) {
	if strings.TrimSpace(r.Name) == "" || r.Size < 0 || strings.ContainsAny(r.ContentType+r.Origin, "\r\n") {
		return "", errors.New("invalid upload session request")
	}
	if r.ContentType == "" {
		r.ContentType = "application/octet-stream"
	}
	uploadBase := strings.TrimSuffix(c.base, "/drive/v3/") + "/upload/drive/v3/files"
	q := "?uploadType=resumable&fields=id,name,mimeType,size,modifiedTime,parents"
	method, endpoint, metadata := http.MethodPost, uploadBase+q, map[string]any{"name": r.Name}
	switch {
	case r.ReplaceID != "":
		if !validID(r.ReplaceID) {
			return "", errors.New("invalid file ID")
		}
		method, endpoint = http.MethodPatch, uploadBase+"/"+url.PathEscape(r.ReplaceID)+q
	case validID(r.ParentID):
		metadata["parents"] = []string{r.ParentID}
	default:
		return "", errors.New("invalid parent folder")
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", r.ContentType)
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(r.Size, 10))
	if r.Origin != "" {
		req.Header.Set("Origin", r.Origin)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{StatusCode: resp.StatusCode}
	}
	// Only hand out a URI on the Drive upload host we called.
	base, _ := url.Parse(uploadBase)
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || base == nil || loc.Scheme != base.Scheme || loc.Host != base.Host || !strings.HasPrefix(loc.Path, base.Path) {
		return "", errors.New("Google Drive returned an unexpected upload location")
	}
	return loc.String(), nil
}
