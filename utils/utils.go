package utils

import (
	"bufio"
	"fmt"
	"os"

	"github.com/cheesycod/mewld/config"
)

func ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// Given a config, return the directory to use
func ConfigGetDirectory(config *config.CoreConfig) (string, error) {
	var dir string
	var err error
	if config.OverrideDir != "" {
		dir = config.OverrideDir
	} else {
		var dirname string
		if config.UseCurrentDirectory {
			dirname, err = os.Getwd()

			if err != nil {
				return "", fmt.Errorf("could not find current directory: %w", err)
			}
		} else {
			dirname, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("could not find home directory: %w", err)
			}
		}

		dir = dirname + "/" + config.Dir
	}

	return dir, nil
}
