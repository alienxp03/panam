package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	maxLines    int
	files       []string
	refreshRate int
	include     string
	exclude     string
	timezone    string
)

var rootCmd = &cobra.Command{
	Use:   "panam [file or directory]",
	Short: "A Terminal User Interface for viewing and filtering log files",
	Long: `Panam is a TUI application for viewing and filtering log files in real-time.
It supports piped input, file reading, and provides an interactive interface
for filtering logs by patterns and log levels.

Usage:
  panam                        # Read from stdin
  panam file.log               # Read single file
  panam /path/to/logs          # Read all files in directory
  panam -e file1.log,file2.log # Read multiple files`,
	Run: func(cmd *cobra.Command, args []string) {
		// Handle positional arguments
		if len(args) > 0 && len(files) == 0 {
			// First argument is treated as file or directory if -e flag not used
			fileInfo, err := os.Stat(args[0])
			if err == nil {
				if fileInfo.IsDir() {
					// If it's a directory, get all files in it
					files = getFilesInDirectory(args[0])
				} else {
					// Single file
					files = []string{args[0]}
				}
			} else {
				// If file doesn't exist, still add it (might be created later)
				files = []string{args[0]}
			}
		}

		config := &Config{
			MaxLines:    maxLines,
			Files:       files,
			RefreshRate: refreshRate,
			Include:     include,
			Exclude:     exclude,
			Timezone:    timezone,
		}

		app := NewApp(config)
		if err := app.Run(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.Flags().IntVarP(&maxLines, "max_line", "m", 50000, "Maximum lines to keep in memory")
	rootCmd.Flags().StringSliceVarP(&files, "files", "e", []string{}, "List of files to process")
	rootCmd.Flags().IntVarP(&refreshRate, "refresh_rate", "r", 1, "Refresh rate in seconds")
	rootCmd.Flags().StringVarP(&include, "include", "i", "", "Default include filter patterns (comma-separated)")
	rootCmd.Flags().StringVarP(&exclude, "exclude", "x", "", "Default exclude filter patterns (comma-separated)")
	rootCmd.Flags().StringVar(&timezone, "timezone", "UTC", "Display timezone for timestamps")
}

func getFilesInDirectory(dir string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}
		if !info.IsDir() {
			// Only add regular files, not directories
			files = append(files, path)
		}
		return nil
	})
	return files
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

