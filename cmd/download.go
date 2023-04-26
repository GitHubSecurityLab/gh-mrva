/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
  "sync"
  "errors"
	"fmt"
  "os"
  "path/filepath"
  "strings"
  "log"
  "github.com/GitHubSecurityLab/gh-mrva/utils"
  "github.com/GitHubSecurityLab/gh-mrva/models"
  "github.com/GitHubSecurityLab/gh-mrva/config"

	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Downloads the artifacts associated to a given session.",
	Long: `Downloads the artifacts associated to a given session.`,
	Run: func(cmd *cobra.Command, args []string) {
    downloadArtifacts()
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringVarP(&sessionNameFlag, "session", "s", "", "Session name to be downloaded")
	downloadCmd.Flags().StringVarP(&outputDirFlag, "output-dir", "o", "", "Output directory")
	downloadCmd.Flags().BoolVarP(&downloadDBsFlag, "download-dbs", "d", false, "Download databases (optional)")
	downloadCmd.Flags().StringVarP(&nwoFlag, "nwo", "n", "", "Repository to download artifacts for (optional)")
	downloadCmd.MarkFlagRequired("session")
	downloadCmd.MarkFlagRequired("output-dir")
}

func downloadArtifacts() {

	// if outputDirFlag does not exist, create it
	if _, err := os.Stat(outputDirFlag); os.IsNotExist(err) {
		err := os.MkdirAll(outputDirFlag, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}

	controller, runs, language, err := utils.LoadSession(sessionNameFlag)
	if err != nil {
		fmt.Println(err)
	} else if len(runs) == 0 {
		fmt.Println("No runs found for sessions" + sessionNameFlag)
	}

	var downloadTasks []models.DownloadTask

	for _, run := range runs {
		runDetails, err := utils.GetRunDetails(controller, run.Id)
		if err != nil {
			log.Fatal(err)
		}
		if runDetails["status"] == "in_progress" {
			log.Printf("Run %d is not complete yet. Please try again later.", run.Id)
			return
		}
		for _, r := range runDetails["scanned_repositories"].([]interface{}) {
			repo := r.(map[string]interface{})
			result_count := repo["result_count"]
			repoInfo := repo["repository"].(map[string]interface{})
			nwo := repoInfo["full_name"].(string)
			// if nwoFlag is set, only download artifacts for that repository
			if nwoFlag != "" && nwoFlag != nwo {
				continue
			}
			if result_count != nil && result_count.(float64) > 0 {
				// check if the SARIF or BQRS file already exists
				dnwo := strings.Replace(nwo, "/", "_", -1)
				sarifPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s.sarif", dnwo))
				bqrsPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s.bqrs", dnwo))
				targetPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s_%s_db.zip", dnwo, language))
				_, bqrsErr := os.Stat(bqrsPath)
				_, sarifErr := os.Stat(sarifPath)
				if errors.Is(bqrsErr, os.ErrNotExist) && errors.Is(sarifErr, os.ErrNotExist) {
					downloadTasks = append(downloadTasks, models.DownloadTask{
						RunId:      run.Id,
						Nwo:        nwo,
						Controller: controller,
						Artifact:   "artifact",
						Language:   language,
						OutputDir:  outputDirFlag,
					})
				}
				if downloadDBsFlag {
					// check if the database already exists
					if _, err := os.Stat(targetPath); errors.Is(err, os.ErrNotExist) {
						downloadTasks = append(downloadTasks, models.DownloadTask{
							RunId:      run.Id,
							Nwo:        nwo,
							Controller: controller,
							Artifact:   "database",
							Language:   language,
							OutputDir:  outputDirFlag,
						})
					}
				}
			}
		}
	}

	wg := new(sync.WaitGroup)

	taskChannel := make(chan models.DownloadTask)
	resultChannel := make(chan models.DownloadTask, len(downloadTasks))

	// Start the workers
	for i := 0; i < config.WORKERS; i++ {
		wg.Add(1)
		go utils.DownloadWorker(wg, taskChannel, resultChannel)
	}

	// Send jobs to worker
	for _, downloadTask := range downloadTasks {
		taskChannel <- downloadTask
	}
	close(taskChannel)

	count := 0
	progressDone := make(chan bool)

	go func() {
		for value := range resultChannel {
			count++
			fmt.Printf("Downloaded %s for %s (%d/%d)\n", value.Artifact, value.Nwo, count, len(downloadTasks))
		}
		fmt.Println(fmt.Sprintf("%d artifacts downloaded", count))
		progressDone <- true
	}()

	// wait for all workers to finish
	wg.Wait()

	// close the result channel
	close(resultChannel)

	// drain the progress channel
	<-progressDone
}
