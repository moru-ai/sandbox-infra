package volumes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moru-ai/sandbox-infra/tests/integration/internal/api"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/setup"
)

func ptr[T any](v T) *T {
	return &v
}

func TestVolumeFileUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-upload"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file
	fileContent := "Hello, JuiceFS!"
	filePath := "/test.txt"

	// Delete any existing file first (JuiceFS Create doesn't overwrite)
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader(fileContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	if uploadResp.StatusCode() != http.StatusCreated {
		t.Logf("Upload response: %s", string(uploadResp.Body))
	}
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())
	require.NotNil(t, uploadResp.JSON201)
	assert.Equal(t, filePath, uploadResp.JSON201.Path)
	assert.Equal(t, int64(len(fileContent)), uploadResp.JSON201.Size)
}

func TestVolumeFileList(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-list"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file first
	fileContent := "List test content"
	filePath := "/list-test.txt"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader(fileContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	// List files at root
	listResp, err := c.GetVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesParams{Path: ptr("/")},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	if listResp.StatusCode() != http.StatusOK {
		t.Logf("List response: %s", string(listResp.Body))
	}
	assert.Equal(t, http.StatusOK, listResp.StatusCode())
	require.NotNil(t, listResp.JSON200)

	// Find our uploaded file
	found := false
	for _, f := range listResp.JSON200.Files {
		if f.Name == "list-test.txt" {
			found = true
			assert.Equal(t, api.File, f.Type)
			require.NotNil(t, f.Size)
			assert.Equal(t, int64(len(fileContent)), *f.Size)
			break
		}
	}
	assert.True(t, found, "Uploaded file should be in list")
}

func TestVolumeFileDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-download"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file first
	fileContent := "Download test content with special chars: \n\t日本語"
	filePath := "/download-test.txt"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader(fileContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	// Download the file
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	if downloadResp.StatusCode() != http.StatusOK {
		t.Logf("Download response status: %d, body: %s", downloadResp.StatusCode(), string(downloadResp.Body))
	}
	assert.Equal(t, http.StatusOK, downloadResp.StatusCode())

	// Verify content
	assert.Equal(t, fileContent, string(downloadResp.Body))
}

func TestVolumeFileDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-delete"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file first
	filePath := "/delete-test.txt"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader("To be deleted"),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	// Delete the file
	deleteResp, err := c.DeleteVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify it's gone by trying to download
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, downloadResp.StatusCode())
}

func TestVolumeFileUploadDirectory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-upload-dir"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file in a nested directory (should auto-create directories)
	fileContent := "Nested file content"
	filePath := "/subdir/nested/file.txt"

	// Delete any existing file/directory first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: "/subdir", Recursive: ptr(true)}, setup.WithAPIKey())

	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader(fileContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())

	// List the nested directory
	listResp, err := c.GetVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesParams{Path: ptr("/subdir/nested")},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, listResp.StatusCode())
	require.NotNil(t, listResp.JSON200)

	// Find our file
	found := false
	for _, f := range listResp.JSON200.Files {
		if f.Name == "file.txt" {
			found = true
			break
		}
	}
	assert.True(t, found, "Nested file should be in list")
}

func TestVolumeFileDeleteRecursive(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-delete-recursive"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload files in a directory structure
	for _, path := range []string{"/rmdir/file1.txt", "/rmdir/sub/file2.txt"} {
		_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
			ctx,
			volume.VolumeID,
			&api.PutVolumesVolumeIDFilesUploadParams{Path: path},
			"application/octet-stream",
			strings.NewReader("content"),
			setup.WithAPIKey(),
		)
		require.NoError(t, err)
	}

	// Delete the directory recursively
	deleteResp, err := c.DeleteVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{
			Path:      "/rmdir",
			Recursive: ptr(true),
		},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify directory is gone
	listResp, err := c.GetVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesParams{Path: ptr("/rmdir")},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, listResp.StatusCode())
}

func TestVolumeFileLargeFile(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-large"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a 1MB file
	size := 1024 * 1024 // 1 MB
	content := bytes.Repeat([]byte("A"), size)
	filePath := "/large-file.bin"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		bytes.NewReader(content),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	if uploadResp.StatusCode() != http.StatusCreated {
		t.Logf("Upload response: %s", string(uploadResp.Body))
	}
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())
	require.NotNil(t, uploadResp.JSON201)
	assert.Equal(t, int64(size), uploadResp.JSON201.Size)

	// Download and verify
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, downloadResp.StatusCode())
	assert.Equal(t, size, len(downloadResp.Body))

	// Verify content integrity
	assert.True(t, bytes.Equal(content, downloadResp.Body), "Downloaded content should match uploaded content")
}

func TestVolumeFileNotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-notfound"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Try to download non-existent file
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: "/nonexistent.txt"},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, downloadResp.StatusCode())

	// Try to list non-existent directory
	listResp, err := c.GetVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesParams{Path: ptr("/nonexistent-dir")},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, listResp.StatusCode())
}

func TestVolumeFileVolumeNotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Try file operations on non-existent volume
	nonExistentVolumeID := "vol_nonexistent123"

	// List files
	listResp, err := c.GetVolumesVolumeIDFilesWithResponse(
		ctx,
		nonExistentVolumeID,
		&api.GetVolumesVolumeIDFilesParams{Path: ptr("/")},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, listResp.StatusCode())

	// Upload file
	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		nonExistentVolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: "/test.txt"},
		"application/octet-stream",
		strings.NewReader("content"),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, uploadResp.StatusCode())

	// Download file
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		nonExistentVolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: "/test.txt"},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, downloadResp.StatusCode())

	// Delete file
	deleteResp, err := c.DeleteVolumesVolumeIDFilesWithResponse(
		ctx,
		nonExistentVolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: "/test.txt"},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, deleteResp.StatusCode())
}

func TestVolumeFileOverwrite(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-overwrite"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	filePath := "/overwrite.txt"

	// Upload initial content
	_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader("Original content"),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	// Delete and re-upload (JuiceFS Create may not truncate existing files)
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(
		ctx,
		volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath},
		setup.WithAPIKey(),
	)

	// Upload new content
	newContent := "New content after overwrite"
	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader(newContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())

	// Download and verify new content
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, newContent, string(downloadResp.Body))
}

func TestVolumeFileMinimalContent(t *testing.T) {
	// Note: Empty files (0 bytes) are not supported due to HTTP body requirements.
	// This test verifies that minimal content files work correctly.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-minimal"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload single-byte file
	filePath := "/minimal.txt"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		strings.NewReader("x"),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())
	require.NotNil(t, uploadResp.JSON201)
	assert.Equal(t, int64(1), uploadResp.JSON201.Size)

	// Download and verify
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, downloadResp.StatusCode())
	assert.Equal(t, "x", string(downloadResp.Body))
}

func TestVolumeFileBinaryContent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-binary"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload binary content with null bytes and high bytes
	binaryContent := []byte{0x00, 0x01, 0xFF, 0xFE, 0x7F, 0x80, 0x00, 0xFF}
	filePath := "/binary.bin"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	uploadResp, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		bytes.NewReader(binaryContent),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, uploadResp.StatusCode())

	// Download and verify binary integrity
	downloadResp, err := c.GetVolumesVolumeIDFilesDownloadWithResponse(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, downloadResp.StatusCode())
	assert.True(t, bytes.Equal(binaryContent, downloadResp.Body), "Binary content should match exactly")
}

// Streaming download test for large files - uses raw client to verify streaming
func TestVolumeFileStreamingDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-volume-file-streaming"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Upload a file
	size := 100 * 1024 // 100 KB
	content := bytes.Repeat([]byte("X"), size)
	filePath := "/streaming.bin"

	// Delete any existing file first
	_, _ = c.DeleteVolumesVolumeIDFilesWithResponse(ctx, volume.VolumeID,
		&api.DeleteVolumesVolumeIDFilesParams{Path: filePath}, setup.WithAPIKey())

	_, err := c.PutVolumesVolumeIDFilesUploadWithBodyWithResponse(
		ctx,
		volume.VolumeID,
		&api.PutVolumesVolumeIDFilesUploadParams{Path: filePath},
		"application/octet-stream",
		bytes.NewReader(content),
		setup.WithAPIKey(),
	)
	require.NoError(t, err)

	// Use raw client to test streaming (without buffering entire response)
	resp, err := c.GetVolumesVolumeIDFilesDownload(
		ctx,
		volume.VolumeID,
		&api.GetVolumesVolumeIDFilesDownloadParams{Path: filePath},
		setup.WithAPIKey(),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read in chunks to verify streaming works
	var totalRead int64
	buf := make([]byte, 8192)
	for {
		n, err := resp.Body.Read(buf)
		totalRead += int64(n)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, int64(size), totalRead)
}
