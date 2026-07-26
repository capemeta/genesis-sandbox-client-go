package sandbox

import (
	"context"
	"io"
)

// FileOption configures file listing behavior.
type FileOption func(*fileOptions)

type fileOptions struct {
	recursive bool
	limit     int
}

// WithRecursive enables recursive file listing.
func WithRecursive() FileOption {
	return func(o *fileOptions) { o.recursive = true }
}

// WithFileLimit sets the maximum entries returned.
func WithFileLimit(n int) FileOption {
	return func(o *fileOptions) { o.limit = n }
}

// FileEntry represents a file or directory in the workspace.
type FileEntry = WorkspaceFileInfo

// Upload writes a file to the workspace.
// If not yet opened, auto-triggers Open() (transparent upgrade to Session mode).
func (sb *Sandbox) Upload(ctx context.Context, path string, content io.Reader) error {
	if err := sb.ensureOpen(ctx); err != nil {
		return err
	}
	_, err := sb.client.UploadSessionFile(ctx, sb.SessionID(), path, content)
	return err
}

// Download reads a file from the workspace.
// If not yet opened, auto-triggers Open().
func (sb *Sandbox) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := sb.ensureOpen(ctx); err != nil {
		return nil, err
	}
	rc, _, err := sb.client.DownloadSessionFile(ctx, sb.SessionID(), path)
	return rc, err
}

// Files lists workspace directory entries.
// If not yet opened, auto-triggers Open().
func (sb *Sandbox) Files(ctx context.Context, path string, opts ...FileOption) ([]FileEntry, error) {
	if err := sb.ensureOpen(ctx); err != nil {
		return nil, err
	}
	fo := &fileOptions{}
	for _, fn := range opts {
		fn(fo)
	}
	result, err := sb.client.ListSessionFiles(ctx, sb.SessionID(), path, fo.recursive, fo.limit)
	if err != nil {
		return nil, err
	}
	return result.Entries, nil
}

// StatFile returns metadata for a workspace file.
// If not yet opened, auto-triggers Open().
func (sb *Sandbox) StatFile(ctx context.Context, path string) (*FileEntry, error) {
	if err := sb.ensureOpen(ctx); err != nil {
		return nil, err
	}
	return sb.client.StatSessionFile(ctx, sb.SessionID(), path)
}

// Mkdir creates a directory tree in the workspace.
// If not yet opened, auto-triggers Open().
func (sb *Sandbox) Mkdir(ctx context.Context, path string) error {
	if err := sb.ensureOpen(ctx); err != nil {
		return err
	}
	_, err := sb.client.MkdirSessionDir(ctx, sb.SessionID(), path)
	return err
}

// Remove deletes a file or directory from the workspace.
// If not yet opened, auto-triggers Open().
func (sb *Sandbox) Remove(ctx context.Context, path string, recursive bool) error {
	if err := sb.ensureOpen(ctx); err != nil {
		return err
	}
	return sb.client.RemoveSessionFile(ctx, sb.SessionID(), path, recursive)
}
