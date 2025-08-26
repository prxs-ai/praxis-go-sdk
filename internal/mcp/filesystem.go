package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
)

type FilesystemTools struct {
	logger  *logrus.Logger
	baseDir string
}

func NewFilesystemTools(logger *logrus.Logger, baseDir string) *FilesystemTools {
	if logger == nil {
		logger = logrus.New()
	}

	if baseDir == "" {
		baseDir = "/shared"
	}

	// Ensure base directory exists
	os.MkdirAll(baseDir, 0755)

	return &FilesystemTools{
		logger:  logger,
		baseDir: baseDir,
	}
}

func (f *FilesystemTools) GetWriteFileTool() mcpTypes.Tool {
	return mcpTypes.NewTool("write_file",
		mcpTypes.WithDescription("Write content to a file in the shared filesystem"),
		mcpTypes.WithString("filename",
			mcpTypes.Required(),
			mcpTypes.Description("Name of the file to create/write"),
		),
		mcpTypes.WithString("content",
			mcpTypes.Required(),
			mcpTypes.Description("Content to write to the file"),
		),
	)
}

func (f *FilesystemTools) WriteFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	filename, _ := args["filename"].(string)
	content, _ := args["content"].(string)

	if filename == "" {
		return mcpTypes.NewToolResultError("filename is required"), nil
	}

	if content == "" {
		return mcpTypes.NewToolResultError("content is required"), nil
	}

	// Sanitize filename to prevent directory traversal
	filename = filepath.Base(filename)
	fullPath := filepath.Join(f.baseDir, filename)

	f.logger.Infof("Writing file: %s", fullPath)

	err := os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		f.logger.Errorf("Failed to write file %s: %v", fullPath, err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to write file: %v", err)), nil
	}

	f.logger.Infof("Successfully wrote file: %s (%d bytes)", fullPath, len(content))

	return mcpTypes.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), filename)), nil
}

func (f *FilesystemTools) GetReadFileTool() mcpTypes.Tool {
	return mcpTypes.NewTool("read_file",
		mcpTypes.WithDescription("Read content from a file in the shared filesystem"),
		mcpTypes.WithString("filename",
			mcpTypes.Required(),
			mcpTypes.Description("Name of the file to read"),
		),
	)
}

func (f *FilesystemTools) ReadFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	filename, _ := args["filename"].(string)

	if filename == "" {
		return mcpTypes.NewToolResultError("filename is required"), nil
	}

	// Sanitize filename to prevent directory traversal
	filename = filepath.Base(filename)
	fullPath := filepath.Join(f.baseDir, filename)

	f.logger.Infof("Reading file: %s", fullPath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		f.logger.Errorf("Failed to read file %s: %v", fullPath, err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	f.logger.Infof("Successfully read file: %s (%d bytes)", fullPath, len(content))

	return mcpTypes.NewToolResultText(string(content)), nil
}

func (f *FilesystemTools) GetListFilesTool() mcpTypes.Tool {
	return mcpTypes.NewTool("list_files",
		mcpTypes.WithDescription("List all files in the shared filesystem"),
	)
}

func (f *FilesystemTools) ListFilesHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	f.logger.Infof("Listing files in: %s", f.baseDir)

	entries, err := os.ReadDir(f.baseDir)
	if err != nil {
		f.logger.Errorf("Failed to list files in %s: %v", f.baseDir, err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to list files: %v", err)), nil
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	result := fmt.Sprintf("Files in shared directory:\n")
	if len(files) == 0 {
		result += "  (no files)"
	} else {
		for _, file := range files {
			result += fmt.Sprintf("  - %s\n", file)
		}
	}

	f.logger.Infof("Found %d files in %s", len(files), f.baseDir)

	return mcpTypes.NewToolResultText(result), nil
}

func (f *FilesystemTools) GetDeleteFileTool() mcpTypes.Tool {
	return mcpTypes.NewTool("delete_file",
		mcpTypes.WithDescription("Delete a file from the shared filesystem"),
		mcpTypes.WithString("filename",
			mcpTypes.Required(),
			mcpTypes.Description("Name of the file to delete"),
		),
	)
}

func (f *FilesystemTools) DeleteFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	filename, _ := args["filename"].(string)

	if filename == "" {
		return mcpTypes.NewToolResultError("filename is required"), nil
	}

	// Sanitize filename to prevent directory traversal
	filename = filepath.Base(filename)
	fullPath := filepath.Join(f.baseDir, filename)

	f.logger.Infof("Deleting file: %s", fullPath)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return mcpTypes.NewToolResultError(fmt.Sprintf("File not found: %s", filename)), nil
	}

	err := os.Remove(fullPath)
	if err != nil {
		f.logger.Errorf("Failed to delete file %s: %v", fullPath, err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to delete file: %v", err)), nil
	}

	f.logger.Infof("Successfully deleted file: %s", fullPath)

	return mcpTypes.NewToolResultText(fmt.Sprintf("Successfully deleted file: %s", filename)), nil
}
