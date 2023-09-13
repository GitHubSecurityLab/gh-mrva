/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"github.com/GitHubSecurityLab/gh-mrva/config"
	"github.com/GitHubSecurityLab/gh-mrva/models"
	"github.com/GitHubSecurityLab/gh-mrva/utils"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Downloads the artifacts associated to a given session.",
	Long:  `Downloads the artifacts associated to a given session.`,
	Run: func(cmd *cobra.Command, args []string) {
		downloadArtifacts()
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringVarP(&sessionNameFlag, "session", "s", "", "Session name to be downloaded")
	downloadCmd.Flags().IntVarP(&runIdFlag, "run", "r", 0, "Run ID to be downloaded")
	downloadCmd.Flags().StringVarP(&outputDirFlag, "output-dir", "o", "", "Output directory")
	downloadCmd.Flags().BoolVarP(&downloadDBsFlag, "download-dbs", "d", false, "Download databases (optional)")
	downloadCmd.Flags().StringVarP(&nwoFlag, "nwo", "n", "", "Repository to download artifacts for (optional)")
	downloadCmd.MarkFlagRequired("output-dir")
	downloadCmd.MarkFlagsMutuallyExclusive("session", "run")
}

func downloadArtifacts() {

	// if outputDirFlag does not exist, create it
	if _, err := os.Stat(outputDirFlag); os.IsNotExist(err) {
		err := os.MkdirAll(outputDirFlag, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}

	controller := ""
	language := ""
	runs := []models.Run{}
	err := error(nil)

	if sessionNameFlag != "" {
		controller, runs, language, err = utils.LoadSession(sessionNameFlag)
		if err != nil {
			fmt.Println(err)
		} else if len(runs) == 0 {
			fmt.Println("No runs found for sessions" + sessionNameFlag)
		}
	} else if runIdFlag > 0 {
		controller, runs, language, err = utils.LoadRun(runIdFlag)
		if err != nil {
			fmt.Println(err)
		}
	} else {
		fmt.Println("Please specify a session or run to download artifacts for")
		return
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
				outputFilename := fmt.Sprintf("%s_%d", nwo, run.Id)
				outputFilename = strings.Replace(outputFilename, "/", "_", -1)
				fmt.Println(fmt.Sprintf("Downloading artifacts for %s", outputFilename))

				// download artifacts if they don't exist
				sarifPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s.sarif", outputFilename))
				bqrsPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s.bqrs", outputFilename))
				_, bqrsErr := os.Stat(bqrsPath)
				_, sarifErr := os.Stat(sarifPath)
				if errors.Is(bqrsErr, os.ErrNotExist) && errors.Is(sarifErr, os.ErrNotExist) {
					downloadTasks = append(downloadTasks, models.DownloadTask{
						RunId:          run.Id,
						Nwo:            nwo,
						Controller:     controller,
						Artifact:       "artifact",
						Language:       language,
						OutputDir:      outputDirFlag,
						OutputFilename: outputFilename,
					})
				}
				dbPath := filepath.Join(outputDirFlag, fmt.Sprintf("%s_%s_db.zip", outputFilename, language))
				if downloadDBsFlag {
					// check if the database already exists
					if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
						downloadTasks = append(downloadTasks, models.DownloadTask{
							RunId:          run.Id,
							Nwo:            nwo,
							Controller:     controller,
							Artifact:       "database",
							Language:       language,
							OutputDir:      outputDirFlag,
							OutputFilename: outputFilename,
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
