package rpc

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// TestNewFileManager validates FileManager creation and default configuration
func TestNewFileManager(t *testing.T) {
	fm := NewFileManager()
	if fm == nil {
		t.Fatal("NewFileManager returned nil")
	}

	// Check default minimum free space (100MB)
	expectedMinFree := int64(100 * 1024 * 1024)
	if fm.minimumFreeSpace != expectedMinFree {
		t.Errorf("Expected default minimum free space %d, got %d", expectedMinFree, fm.minimumFreeSpace)
	}
}

// TestSetMinimumFreeSpace validates minimum free space configuration
func TestSetMinimumFreeSpace(t *testing.T) {
	fm := NewFileManager()

	testValues := []int64{0, 1024, 50 * 1024 * 1024, 1024 * 1024 * 1024}
	for _, value := range testValues {
		fm.SetMinimumFreeSpace(value)
		if fm.minimumFreeSpace != value {
			t.Errorf("Expected minimum free space %d, got %d", value, fm.minimumFreeSpace)
		}
	}
}

// TestCheckDiskSpace validates disk space checking functionality
func TestCheckDiskSpace(t *testing.T) {
	fm := NewFileManager()
	fm.SetMinimumFreeSpace(1024) // Set low minimum for testing

	// Test with current directory (should have space)
	err := fm.CheckDiskSpace(".", 1024)
	if err != nil {
		t.Errorf("CheckDiskSpace failed for current directory: %v", err)
	}

	// Test with non-existent directory (should fail)
	err = fm.CheckDiskSpace("/non/existent/path", 1024)
	if err == nil {
		t.Error("CheckDiskSpace should fail for non-existent directory")
	}
}

// TestGetDiskSpace validates disk space reporting
func TestGetDiskSpace(t *testing.T) {
	fm := NewFileManager()

	total, available, err := fm.GetDiskSpace(".")
	if err != nil {
		t.Errorf("GetDiskSpace failed: %v", err)
	}

	if total <= 0 {
		t.Errorf("Total disk space should be positive, got %d", total)
	}

	if available <= 0 {
		t.Errorf("Available disk space should be positive, got %d", available)
	}

	if available > total {
		t.Errorf("Available space (%d) cannot exceed total space (%d)", available, total)
	}
}

// TestMoveFilesSingleFile tests moving a single file torrent
func TestMoveFilesSingleFile(t *testing.T) {
	fm := NewFileManager()
	fm.SetMinimumFreeSpace(0) // Disable space check for test

	// Create temporary directories
	sourceDir := t.TempDir()
	destDir := t.TempDir()

	// Create test file
	testContent := "test file content"
	sourceFile := filepath.Join(sourceDir, "test.txt")
	if err := os.WriteFile(sourceFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create mock single-file torrent info
	info := metainfo.Info{
		Name:        "test.txt",
		Length:      int64(len(testContent)),
		PieceLength: 16384,
	}

	// Move the file
	err := fm.MoveFiles(info, sourceDir, destDir, true)
	if err != nil {
		t.Errorf("MoveFiles failed: %v", err)
	}

	// Verify file was moved
	destFile := filepath.Join(destDir, "test.txt")
	if _, err := os.Stat(destFile); err != nil {
		t.Errorf("Destination file does not exist: %v", err)
	}

	// Verify source file was removed
	if _, err := os.Stat(sourceFile); !os.IsNotExist(err) {
		t.Error("Source file should have been removed")
	}

	// Verify content is correct
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Errorf("Failed to read moved file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("File content mismatch: expected %q, got %q", testContent, string(content))
	}
}

// TestMoveFilesMultiFile tests moving a multi-file torrent
func TestMoveFilesMultiFile(t *testing.T) {
	fm := NewFileManager()
	fm.SetMinimumFreeSpace(0)

	sourceDir := t.TempDir()
	destDir := t.TempDir()

	// Create test directory structure
	torrentName := "test_torrent"
	torrentDir := filepath.Join(sourceDir, torrentName)
	if err := os.MkdirAll(torrentDir, 0755); err != nil {
		t.Fatalf("Failed to create torrent directory: %v", err)
	}

	// Create test files
	files := []struct {
		path    string
		content string
	}{
		{"file1.txt", "content of file 1"},
		{"subdir/file2.txt", "content of file 2"},
		{"subdir/file3.txt", "content of file 3"},
	}

	var totalLength int64
	for _, file := range files {
		fullPath := filepath.Join(torrentDir, file.path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", file.path, err)
		}
		if err := os.WriteFile(fullPath, []byte(file.content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", file.path, err)
		}
		totalLength += int64(len(file.content))
	}

	// Create mock multi-file torrent info
	info := metainfo.Info{
		Name:        torrentName,
		PieceLength: 16384,
		Files: []metainfo.File{
			{Length: int64(len(files[0].content)), Paths: []string{"file1.txt"}},
			{Length: int64(len(files[1].content)), Paths: []string{"subdir", "file2.txt"}},
			{Length: int64(len(files[2].content)), Paths: []string{"subdir", "file3.txt"}},
		},
	}

	// Move files with structure preservation
	err := fm.MoveFiles(info, sourceDir, destDir, true)
	if err != nil {
		t.Errorf("MoveFiles failed: %v", err)
	}

	// Verify all files were moved correctly
	for _, file := range files {
		destFile := filepath.Join(destDir, torrentName, file.path)
		if _, err := os.Stat(destFile); err != nil {
			t.Errorf("Destination file %s does not exist: %v", destFile, err)
		}

		// Verify content
		content, err := os.ReadFile(destFile)
		if err != nil {
			t.Errorf("Failed to read moved file %s: %v", destFile, err)
		}
		if string(content) != file.content {
			t.Errorf("File content mismatch for %s: expected %q, got %q", file.path, file.content, string(content))
		}
	}
}

// TestMoveFilesInsufficientSpace tests space checking during file moves
func TestMoveFilesInsufficientSpace(t *testing.T) {
	fm := NewFileManager()
	fm.SetMinimumFreeSpace(1024 * 1024 * 1024 * 1024) // Set very high minimum (1TB)

	sourceDir := t.TempDir()
	destDir := t.TempDir()

	// Create small test file
	sourceFile := filepath.Join(sourceDir, "test.txt")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	info := metainfo.Info{
		Name:        "test.txt",
		Length:      4,
		PieceLength: 16384,
	}

	// Should fail due to insufficient space
	err := fm.MoveFiles(info, sourceDir, destDir, true)
	if err == nil {
		t.Error("MoveFiles should fail when there's insufficient disk space")
	}
	if !strings.Contains(err.Error(), "insufficient disk space") {
		t.Errorf("Expected insufficient disk space error, got: %v", err)
	}
}

// TestMoveFilesNonExistentSource tests error handling for missing source
func TestMoveFilesNonExistentSource(t *testing.T) {
	fm := NewFileManager()
	destDir := t.TempDir()

	info := metainfo.Info{
		Name:        "test.txt",
		Length:      4,
		PieceLength: 16384,
	}

	err := fm.MoveFiles(info, "/non/existent/source", destDir, true)
	if err == nil {
		t.Error("MoveFiles should fail for non-existent source directory")
	}
	if !strings.Contains(err.Error(), "source directory not accessible") {
		t.Errorf("Expected source directory error, got: %v", err)
	}
}

// TestVerifyFile tests file integrity verification
func TestVerifyFile(t *testing.T) {
	fm := NewFileManager()

	// Create test file
	testContent := "test file for verification"
	testFile := filepath.Join(t.TempDir(), "verify_test.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Calculate expected hash
	hasher := sha1.New()
	hasher.Write([]byte(testContent))
	expectedHash := hasher.Sum(nil)

	// Verify with correct hash
	err := fm.VerifyFile(testFile, expectedHash)
	if err != nil {
		t.Errorf("VerifyFile failed with correct hash: %v", err)
	}

	// Verify with incorrect hash
	wrongHash := make([]byte, 20) // Wrong hash (all zeros)
	err = fm.VerifyFile(testFile, wrongHash)
	if err == nil {
		t.Error("VerifyFile should fail with incorrect hash")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("Expected verification failed error, got: %v", err)
	}

	// Verify non-existent file
	err = fm.VerifyFile("/non/existent/file", expectedHash)
	if err == nil {
		t.Error("VerifyFile should fail for non-existent file")
	}
}

// TestRenamePartialFile tests partial file renaming
func TestRenamePartialFile(t *testing.T) {
	fm := NewFileManager()
	tempDir := t.TempDir()

	partialFile := filepath.Join(tempDir, "test.txt.part")
	finalFile := filepath.Join(tempDir, "test.txt")

	// Create partial file
	testContent := "partial file content"
	if err := os.WriteFile(partialFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create partial file: %v", err)
	}

	// Rename to final
	err := fm.RenamePartialFile(partialFile, finalFile)
	if err != nil {
		t.Errorf("RenamePartialFile failed: %v", err)
	}

	// Verify final file exists
	if _, err := os.Stat(finalFile); err != nil {
		t.Errorf("Final file does not exist: %v", err)
	}

	// Verify partial file was removed
	if _, err := os.Stat(partialFile); !os.IsNotExist(err) {
		t.Error("Partial file should have been removed")
	}

	// Verify content
	content, err := os.ReadFile(finalFile)
	if err != nil {
		t.Errorf("Failed to read final file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Content mismatch: expected %q, got %q", testContent, string(content))
	}
}

// TestRenamePartialFileErrors tests error conditions for partial file renaming
func TestRenamePartialFileErrors(t *testing.T) {
	fm := NewFileManager()
	tempDir := t.TempDir()

	// Test with non-existent partial file
	err := fm.RenamePartialFile("/non/existent.part", filepath.Join(tempDir, "final.txt"))
	if err == nil {
		t.Error("RenamePartialFile should fail for non-existent partial file")
	}

	// Test with existing destination file
	partialFile := filepath.Join(tempDir, "test.part")
	finalFile := filepath.Join(tempDir, "test.txt")

	// Create both files
	if err := os.WriteFile(partialFile, []byte("partial"), 0644); err != nil {
		t.Fatalf("Failed to create partial file: %v", err)
	}
	if err := os.WriteFile(finalFile, []byte("existing"), 0644); err != nil {
		t.Fatalf("Failed to create final file: %v", err)
	}

	err = fm.RenamePartialFile(partialFile, finalFile)
	if err == nil {
		t.Error("RenamePartialFile should fail when destination exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected 'already exists' error, got: %v", err)
	}
}

// TestCleanupPartialFiles tests cleanup of partial files
func TestCleanupPartialFiles(t *testing.T) {
	fm := NewFileManager()
	tempDir := t.TempDir()

	// Test single file torrent cleanup
	t.Run("SingleFile", func(t *testing.T) {
		partialFile := filepath.Join(tempDir, "single.txt.part")
		if err := os.WriteFile(partialFile, []byte("partial"), 0644); err != nil {
			t.Fatalf("Failed to create partial file: %v", err)
		}

		info := metainfo.Info{
			Name:        "single.txt",
			Length:      7,
			PieceLength: 16384,
		}

		err := fm.CleanupPartialFiles(tempDir, info)
		if err != nil {
			t.Errorf("CleanupPartialFiles failed: %v", err)
		}

		// Verify partial file was removed
		if _, err := os.Stat(partialFile); !os.IsNotExist(err) {
			t.Error("Partial file should have been removed")
		}
	})

	// Test multi-file torrent cleanup
	t.Run("MultiFile", func(t *testing.T) {
		torrentName := "multi_torrent"
		torrentDir := filepath.Join(tempDir, torrentName)
		if err := os.MkdirAll(filepath.Join(torrentDir, "subdir"), 0755); err != nil {
			t.Fatalf("Failed to create torrent directory: %v", err)
		}

		// Create partial files
		partialFiles := []string{
			filepath.Join(torrentDir, "file1.txt.part"),
			filepath.Join(torrentDir, "subdir", "file2.txt.part"),
		}

		for _, pf := range partialFiles {
			if err := os.WriteFile(pf, []byte("partial"), 0644); err != nil {
				t.Fatalf("Failed to create partial file %s: %v", pf, err)
			}
		}

		info := metainfo.Info{
			Name:        torrentName,
			PieceLength: 16384,
			Files: []metainfo.File{
				{Length: 7, Paths: []string{"file1.txt"}},
				{Length: 7, Paths: []string{"subdir", "file2.txt"}},
			},
		}

		err := fm.CleanupPartialFiles(tempDir, info)
		if err != nil {
			t.Errorf("CleanupPartialFiles failed: %v", err)
		}

		// Verify all partial files were removed
		for _, pf := range partialFiles {
			if _, err := os.Stat(pf); !os.IsNotExist(err) {
				t.Errorf("Partial file %s should have been removed", pf)
			}
		}
	})
}

// TestNotifyFileComplete tests file completion notifications
func TestNotifyFileComplete(t *testing.T) {
	fm := NewFileManager()

	var callbackPath string
	var callbackTime time.Time
	callbackCalled := false

	callback := func(path string, completionTime time.Time) {
		callbackPath = path
		callbackTime = completionTime
		callbackCalled = true
	}

	testPath := "/test/file.txt"
	beforeCall := time.Now()

	fm.NotifyFileComplete(testPath, callback)

	afterCall := time.Now()

	if !callbackCalled {
		t.Error("Callback was not called")
	}

	if callbackPath != testPath {
		t.Errorf("Expected callback path %s, got %s", testPath, callbackPath)
	}

	if callbackTime.Before(beforeCall) || callbackTime.After(afterCall) {
		t.Error("Callback time is not within expected range")
	}

	// Test with nil callback (should not panic)
	fm.NotifyFileComplete(testPath, nil)
}

// TestValidateFileName tests filename validation
func TestValidateFileName(t *testing.T) {
	fm := NewFileManager()

	// Valid filenames
	validNames := []string{
		"normal.txt",
		"file-with-dashes.dat",
		"file_with_underscores.bin",
		"file with spaces.doc",
		"123456.numbers",
		"a.b.c.multiple.dots",
	}

	for _, name := range validNames {
		if err := fm.ValidateFileName(name); err != nil {
			t.Errorf("ValidateFileName should accept valid name %q: %v", name, err)
		}
	}

	// Invalid filenames
	invalidTests := []struct {
		name        string
		expectedErr string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"file<invalid.txt", "invalid character"},
		{"file>invalid.txt", "invalid character"},
		{"file:invalid.txt", "invalid character"},
		{"file\"invalid.txt", "invalid character"},
		{"file|invalid.txt", "invalid character"},
		{"file?invalid.txt", "invalid character"},
		{"file*invalid.txt", "invalid character"},
		{"CON", "reserved name"},
		{"PRN.txt", "reserved name"},
		{"con", "reserved name"}, // Case insensitive
		{strings.Repeat("a", 256), "too long"},
	}

	for _, test := range invalidTests {
		err := fm.ValidateFileName(test.name)
		if err == nil {
			t.Errorf("ValidateFileName should reject invalid name %q", test.name)
		}
		if !strings.Contains(err.Error(), test.expectedErr) {
			t.Errorf("Expected error containing %q for name %q, got: %v", test.expectedErr, test.name, err)
		}
	}
}

// TestCopyFile tests the internal copyFile method
func TestCopyFile(t *testing.T) {
	fm := NewFileManager()
	tempDir := t.TempDir()

	sourceFile := filepath.Join(tempDir, "source.txt")
	destFile := filepath.Join(tempDir, "dest.txt")

	testContent := "test content for copy"
	if err := os.WriteFile(sourceFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	err := fm.copyFile(sourceFile, destFile)
	if err != nil {
		t.Errorf("copyFile failed: %v", err)
	}

	// Verify destination file exists and has correct content
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Errorf("Failed to read destination file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Content mismatch: expected %q, got %q", testContent, string(content))
	}

	// Verify file mode is preserved
	sourceInfo, err := os.Stat(sourceFile)
	if err != nil {
		t.Fatalf("Failed to stat source file: %v", err)
	}
	destInfo, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}
	if sourceInfo.Mode() != destInfo.Mode() {
		t.Errorf("File mode not preserved: expected %v, got %v", sourceInfo.Mode(), destInfo.Mode())
	}
}

// TestMoveFileErrors tests error conditions in moveFile
func TestMoveFileErrors(t *testing.T) {
	fm := NewFileManager()

	// Test with non-existent source file
	err := fm.moveFile("/non/existent/source", "/tmp/dest")
	if err == nil {
		t.Error("moveFile should fail for non-existent source")
	}
}

// Benchmark tests for performance validation
func BenchmarkCheckDiskSpace(b *testing.B) {
	fm := NewFileManager()
	for i := 0; i < b.N; i++ {
		fm.CheckDiskSpace(".", 1024)
	}
}

func BenchmarkValidateFileName(b *testing.B) {
	fm := NewFileManager()
	for i := 0; i < b.N; i++ {
		fm.ValidateFileName("test_file.txt")
	}
}
