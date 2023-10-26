/*
Copyright © 2023 Alvaro Munoz pwntester@github.com

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"github.com/GitHubSecurityLab/gh-mrva/utils"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	sessionNameFlag     string
	runIdFlag           int
	sessionPrefixFlag   string
	outputDirFlag       string
	downloadDBsFlag     bool
	nwoFlag             string
	jsonFlag            bool
	languageFlag        string
	listFileFlag        string
	listFlag            string
	codeqlPathFlag      string
	controllerFlag      string
	queryFileFlag       string
	querySuiteFileFlag  string
	additionalPacksFlag string
)
var rootCmd = &cobra.Command{
	Use:   "gh-mrva",
	Short: "Run CodeQL queries at scale using GitHub's Multi-Repository Variant Analysis (MRVA)",
	Long:  `Run CodeQL queries at scale using GitHub's Multi-Repository Variant Analysis (MRVA)`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		homePath := os.Getenv("HOME")
		if homePath == "" {
			log.Fatal("HOME environment variable not set")
		}
		configPath = filepath.Join(homePath, ".config")
	}
	configFilePath := filepath.Join(configPath, "gh-mrva", "config.yml")
	utils.SetConfigFilePath(configFilePath)

	sessionsFilePath := filepath.Join(configPath, "gh-mrva", "sessions.yml")
	if _, err := os.Stat(sessionsFilePath); os.IsNotExist(err) {
		err := os.MkdirAll(filepath.Dir(sessionsFilePath), os.ModePerm)
		if err != nil {
			log.Fatal("Failed to create config directory")
		}
		// create empty file at sessionsFilePath
		sessionsFile, err := os.Create(sessionsFilePath)
		if err != nil {
			log.Fatal("Failed to create sessions file")
		}
		sessionsFile.Close()
	}
	utils.SetSessionsFilePath(sessionsFilePath)
}
