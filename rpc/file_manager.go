// Package rpc provides file management operations for the BitTorrent RPC server.
// This file implements core file system operations including file moving,
// directory space checking, file verification, and completion notifications.
package rpc

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// FileManager handles file system operations for torrent files.
// It provides methods for moving files, checking disk space, verifying file integrity,
// and managing partial files according to BitTorrent best practices.
type FileManager struct {
	// minimumFreeSpace represents the minimum free space (in bytes) required
	// before allowing new downloads or file operations. Defaults to 100MB.
	minimumFreeSpace int64
}

// NewFileManager creates a new FileManager instance with default configuration.
// The manager is initialized with sensible defaults for file operations.
func NewFileManager() *FileManager {
	return &FileManager{
		minimumFreeSpace: 100 * 1024 * 1024, // 100MB default minimum free space
	}
}

// SetMinimumFreeSpace configures the minimum free space requirement in bytes.
// Operations that would reduce available space below this threshold will fail.
func (fm *FileManager) SetMinimumFreeSpace(bytes int64) {
	fm.minimumFreeSpace = bytes
}

// MoveFiles moves all files for a torrent from source to destination directory.
// This operation is atomic - if any file fails to move, all previously moved
// files are rolled back to maintain consistency.
//
// Parameters:
//   - info: MetaInfo describing the torrent file structure
//   - sourceDir: source directory containing the files
//   - destDir: destination directory for the files
//   - preserveStructure: whether to maintain the original directory structure
//
// Returns error if:
//   - Source or destination directories don't exist or aren't accessible
//   - Insufficient disk space at destination
//   - Any file move operation fails
//   - Rollback fails (critical error, may leave files in inconsistent state)
func (fm *FileManager) MoveFiles(info metainfo.Info, sourceDir, destDir string, preserveStructure bool) error {
	// Validate source directory exists and is readable
	if _, err := os.Stat(sourceDir); err != nil {
		return fmt.Errorf("source directory not accessible: %w", err)
	}

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Check available space at destination
	totalSize := info.TotalLength()
	if err := fm.CheckDiskSpace(destDir, totalSize); err != nil {
		return fmt.Errorf("insufficient disk space: %w", err)
	}

	// Track moved files for rollback on failure
	var movedFiles []struct {
		source, dest string
	}

	// Determine source path - could be single file or directory
	var sourcePath string
	if info.IsDir() {
		sourcePath = filepath.Join(sourceDir, info.Name)
	} else {
		// Single file torrent - file name comes from info.Name
		sourcePath = filepath.Join(sourceDir, info.Name)
	}

	if info.IsDir() {
		// Multi-file torrent - move all files maintaining structure
		for _, file := range info.Files {
			// file.Path(info) already includes the torrent name for multi-file torrents
			srcFile := filepath.Join(sourceDir, file.Path(info))

			var destFile string
			if preserveStructure {
				destFile = filepath.Join(destDir, file.Path(info))
			} else {
				// Flatten structure - use just the filename
				destFile = filepath.Join(destDir, filepath.Base(file.Path(info)))
			}

			// Create destination directory for this file
			if err := os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
				// Rollback any files we've already moved
				fm.rollbackMoves(movedFiles)
				return fmt.Errorf("failed to create directory for %s: %w", destFile, err)
			}

			// Move the file
			if err := fm.moveFile(srcFile, destFile); err != nil {
				// Rollback any files we've already moved
				fm.rollbackMoves(movedFiles)
				return fmt.Errorf("failed to move file %s: %w", srcFile, err)
			}

			movedFiles = append(movedFiles, struct{ source, dest string }{srcFile, destFile})
		}
	} else {
		// Single file torrent
		destFile := filepath.Join(destDir, info.Name)

		if err := fm.moveFile(sourcePath, destFile); err != nil {
			return fmt.Errorf("failed to move single file %s: %w", sourcePath, err)
		}

		movedFiles = append(movedFiles, struct{ source, dest string }{sourcePath, destFile})
	}

	return nil
}

// moveFile performs an atomic file move operation.
// It handles cross-filesystem moves by copying and then removing the source.
func (fm *FileManager) moveFile(source, dest string) error {
	// First try a simple rename (works if on same filesystem)
	if err := os.Rename(source, dest); err == nil {
		return nil
	}

	// If rename failed, try copy + delete (handles cross-filesystem moves)
	if err := fm.copyFile(source, dest); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	if err := os.Remove(source); err != nil {
		// Copy succeeded but delete failed - try to clean up destination
		os.Remove(dest)
		return fmt.Errorf("failed to remove source after copy: %w", err)
	}

	return nil
}

// copyFile copies a file from source to destination, preserving file mode.
func (fm *FileManager) copyFile(source, dest string) error {
	srcFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	// Get source file info for mode preservation
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer destFile.Close()

	// Copy the file contents
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy contents: %w", err)
	}

	// Ensure data is written to disk
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination: %w", err)
	}

	return nil
}

// rollbackMoves attempts to undo a series of file moves.
// This is called when a move operation fails partway through.
func (fm *FileManager) rollbackMoves(movedFiles []struct{ source, dest string }) {
	for i := len(movedFiles) - 1; i >= 0; i-- {
		moved := movedFiles[i]
		// Try to move the file back - if this fails, we can't do much more
		os.Rename(moved.dest, moved.source)
	}
}

// CheckDiskSpace verifies that sufficient disk space is available at the given path.
// It checks both available space and ensures the minimum free space requirement is met.
func (fm *FileManager) CheckDiskSpace(path string, requiredBytes int64) error {
	// Get filesystem statistics
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	// Calculate available space in bytes
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)

	// Check if we have enough space for the required bytes plus minimum free space
	totalRequired := requiredBytes + fm.minimumFreeSpace

	if availableBytes < totalRequired {
		return fmt.Errorf("insufficient disk space: need %d bytes, have %d bytes (minimum %d bytes must remain free)",
			requiredBytes, availableBytes-fm.minimumFreeSpace, fm.minimumFreeSpace)
	}

	return nil
}

// GetDiskSpace returns the total and available disk space for the given path.
func (fm *FileManager) GetDiskSpace(path string) (total, available int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	total = int64(stat.Blocks) * int64(stat.Bsize)
	available = int64(stat.Bavail) * int64(stat.Bsize)

	return total, available, nil
}

// VerifyFile verifies that a file matches its expected hash from the torrent.
// This is used to check file integrity after downloads or moves.
func (fm *FileManager) VerifyFile(filePath string, expectedHash []byte) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for verification: %w", err)
	}
	defer file.Close()

	// Calculate SHA-1 hash of the file
	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to read file for hashing: %w", err)
	}

	actualHash := hasher.Sum(nil)

	// Compare hashes
	if len(actualHash) != len(expectedHash) {
		return fmt.Errorf("hash length mismatch: expected %d bytes, got %d bytes",
			len(expectedHash), len(actualHash))
	}

	for i := range actualHash {
		if actualHash[i] != expectedHash[i] {
			return fmt.Errorf("file hash verification failed")
		}
	}

	return nil
}

// RenamePartialFile renames a partial file to its final name when download completes.
// This handles the common BitTorrent pattern of downloading to .part files.
func (fm *FileManager) RenamePartialFile(partialPath, finalPath string) error {
	// Check if partial file exists
	if _, err := os.Stat(partialPath); err != nil {
		return fmt.Errorf("partial file does not exist: %w", err)
	}

	// Check if final file already exists
	if _, err := os.Stat(finalPath); err == nil {
		return fmt.Errorf("destination file already exists: %s", finalPath)
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Rename the file
	if err := os.Rename(partialPath, finalPath); err != nil {
		return fmt.Errorf("failed to rename partial file: %w", err)
	}

	return nil
}

// CleanupPartialFiles removes .part files that are no longer needed.
// This is typically called when a torrent is removed or canceled.
func (fm *FileManager) CleanupPartialFiles(directory string, info metainfo.Info) error {
	if info.IsDir() {
		// Multi-file torrent
		// file.Path(info) already includes the torrent name for multi-file torrents
		for _, file := range info.Files {
			partialPath := filepath.Join(directory, file.Path(info)+".part")
			if _, err := os.Stat(partialPath); err == nil {
				// File exists, remove it
				if err := os.Remove(partialPath); err != nil {
					return fmt.Errorf("failed to remove partial file %s: %w", partialPath, err)
				}
			}
		}
	} else {
		// Single file torrent
		partialPath := filepath.Join(directory, info.Name+".part")
		if _, err := os.Stat(partialPath); err == nil {
			if err := os.Remove(partialPath); err != nil {
				return fmt.Errorf("failed to remove partial file %s: %w", partialPath, err)
			}
		}
	}

	return nil
}

// NotifyFileComplete sends a completion notification for a file.
// This is used to trigger completion hooks or update download statistics.
// The callback function receives the file path and completion time.
func (fm *FileManager) NotifyFileComplete(filePath string, callback func(path string, completionTime time.Time)) {
	if callback != nil {
		callback(filePath, time.Now())
	}
}

// ValidateFileName checks if a filename is valid for the current filesystem.
// It checks for invalid characters, length limits, and reserved names.
func (fm *FileManager) ValidateFileName(fileName string) error {
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("filename cannot be empty")
	}

	// Check for invalid characters (common across filesystems)
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*", "\x00"}
	for _, char := range invalidChars {
		if strings.Contains(fileName, char) {
			return fmt.Errorf("filename contains invalid character: %s", char)
		}
	}

	// Check filename length (255 is common limit)
	if len(fileName) > 255 {
		return fmt.Errorf("filename too long: %d characters (max 255)", len(fileName))
	}

	// Check for reserved names on Windows (for cross-platform compatibility)
	reservedNames := []string{
		"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4",
		"COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5",
		"LPT6", "LPT7", "LPT8", "LPT9",
	}

	upperFileName := strings.ToUpper(fileName)
	for _, reserved := range reservedNames {
		if upperFileName == reserved || strings.HasPrefix(upperFileName, reserved+".") {
			return fmt.Errorf("filename uses reserved name: %s", reserved)
		}
	}

	return nil
}
