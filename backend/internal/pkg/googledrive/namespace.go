package googledrive

import (
	"context"
	"errors"
	"strings"
)

const FolderMimeType = "application/vnd.google-apps.folder"

var ErrPathNotFound = errors.New("Drive path not found")
var ErrAmbiguousPath = errors.New("multiple Drive entries have the same name")

// ResolvePath walks only children of rootID. It fails closed on duplicate
// names rather than choosing an arbitrary file with a potentially different owner.
func (c *Client) ResolvePath(ctx context.Context, rootID, path string) (*File, error) {
	if !validID(rootID) || !strings.HasPrefix(path, "/") {
		return nil, errors.New("invalid Drive path")
	}
	if path == "/" {
		return &File{ID: rootID, Name: "/", MimeType: FolderMimeType}, nil
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 128 {
		return nil, errors.New("Drive path too deep")
	}
	parent := rootID
	var found *File
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 1024 || strings.ContainsAny(part, "\x00\\") {
			return nil, errors.New("invalid Drive path segment")
		}
		found = nil
		pageToken := ""
		seenPages := map[string]bool{}
		for {
			page, err := c.ListChildren(ctx, parent, pageToken, 100)
			if err != nil {
				return nil, err
			}
			for j := range page.Files {
				if page.Files[j].Name == part {
					if found != nil {
						return nil, ErrAmbiguousPath
					}
					item := page.Files[j]
					found = &item
				}
			}
			if page.NextPageToken == "" {
				break
			}
			if len(seenPages) >= 1000 {
				return nil, errors.New("Drive folder exceeds page limit")
			}
			if seenPages[page.NextPageToken] {
				return nil, errors.New("repeated Drive page token")
			}
			seenPages[page.NextPageToken] = true
			pageToken = page.NextPageToken
		}
		if found == nil || !validID(found.ID) {
			return nil, ErrPathNotFound
		}
		if i < len(parts)-1 && found.MimeType != FolderMimeType {
			return nil, ErrPathNotFound
		}
		parent = found.ID
	}
	return found, nil
}
