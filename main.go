package main

import (
	"fmt"
	"os"

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
	Use:   "panam",
	Short: "A Terminal User Interface for viewing and filtering log files",
	Long: `Panam is a TUI application for viewing and filtering log files in real-time.
It supports piped input, file reading, and provides an interactive interface
for filtering logs by patterns and log levels.`,
	Run: func(cmd *cobra.Command, args []string) {
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
	rootCmd.Flags().IntVarP(&maxLines, "max_line", "m", 10000, "Maximum lines to keep in memory")
	rootCmd.Flags().StringSliceVarP(&files, "files", "e", []string{}, "List of files to process")
	rootCmd.Flags().IntVarP(&refreshRate, "refresh_rate", "r", 1, "Refresh rate in seconds")
	rootCmd.Flags().StringVarP(&include, "include", "i", "", "Default include filter patterns (comma-separated)")
	rootCmd.Flags().StringVarP(&exclude, "exclude", "x", "", "Default exclude filter patterns (comma-separated)")
	rootCmd.Flags().StringVar(&timezone, "timezone", "UTC", "Display timezone for timestamps")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

