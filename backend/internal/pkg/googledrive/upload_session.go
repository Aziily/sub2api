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
	ParentID    string // folder that receives the new file
	ContentType string
	Size        int64
	// AppProperties are private to this OAuth client; the pool uses them to
	// find the object a client uploaded without trusting the client's word.
	AppProperties map[string]string
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
	if !validID(r.ParentID) {
		return "", errors.New("invalid parent folder")
	}
	method, endpoint := http.MethodPost, uploadBase+"?uploadType=resumable&fields="+fileFields
	metadata := map[string]any{"name": r.Name, "parents": []string{r.ParentID}}
	if len(r.AppProperties) > 0 {
		metadata["appProperties"] = r.AppProperties
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

// UploadToSession sends size bytes from content to a session created by
// CreateUploadSession and returns the stored file. Content is streamed in a
// single request; the caller enforces its own limits.
func (c *Client) UploadToSession(ctx context.Context, sessionURI string, size int64, content io.Reader) (*File, error) {
	if size < 0 || content == nil {
		return nil, errors.New("invalid upload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, sessionURI, io.LimitReader(content, size))
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	}
	// The session URI already authorizes the upload; the OAuth transport adds
	// a bearer token as well, which Drive accepts.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Drive upload failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var file File
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return nil, errors.New("invalid Google Drive response")
	}
	if file.ID == "" {
		return nil, errors.New("Google Drive returned no file")
	}
	return &file, nil
}
