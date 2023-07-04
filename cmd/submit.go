/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/GitHubSecurityLab/gh-mrva/config"
	"github.com/GitHubSecurityLab/gh-mrva/models"
	"github.com/GitHubSecurityLab/gh-mrva/utils"
	"github.com/spf13/cobra"
)

var (
	controller     string
	codeqlPath     string
	listFile       string
	listName       string
	language       string
	sessionName    string
	queryFile      string
	querySuiteFile string
)
var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a query or query suite to a MRVA controller.",
	Long:  `Submit a query or query suite to a MRVA controller.`,
	Run: func(cmd *cobra.Command, args []string) {
		submitQuery()
	},
}

func init() {
	rootCmd.AddCommand(submitCmd)
	submitCmd.Flags().StringVarP(&sessionNameFlag, "session", "s", "", "Session name")
	submitCmd.Flags().StringVarP(&languageFlag, "language", "l", "", "DB language")
	submitCmd.Flags().StringVarP(&queryFileFlag, "query", "q", "", "Path to query file")
	submitCmd.Flags().StringVarP(&querySuiteFileFlag, "query-suite", "x", "", "Path to query suite file")
	submitCmd.Flags().StringVarP(&controllerFlag, "controller", "c", "", "MRVA controller repository (overrides config file)")
	submitCmd.Flags().StringVarP(&listFileFlag, "list-file", "f", "", "Path to repo list file (overrides config file)")
	submitCmd.Flags().StringVarP(&listFlag, "list", "i", "", "Name of repo list")
	submitCmd.Flags().StringVarP(&codeqlPathFlag, "codeql-path", "p", "", "Path to CodeQL distribution (overrides config file)")
	submitCmd.MarkFlagRequired("session")
	submitCmd.MarkFlagRequired("language")
	submitCmd.MarkFlagsMutuallyExclusive("query", "query-suite")
}

func submitQuery() {
	configData, err := utils.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	if controllerFlag != "" {
		controller = controllerFlag
	} else if configData.Controller != "" {
		controller = configData.Controller
	}
	if listFileFlag != "" {
		listFile = listFileFlag
	} else if configData.ListFile != "" {
		listFile = configData.ListFile
	}
	if codeqlPathFlag != "" {
		codeqlPath = codeqlPathFlag
	} else if configData.CodeQLPath != "" {
		codeqlPath = configData.CodeQLPath
	}
	if languageFlag != "" {
		language = languageFlag
	}
	if sessionNameFlag != "" {
		sessionName = sessionNameFlag
	}
	if listFlag != "" {
		listName = listFlag
	}
	if queryFileFlag != "" {
		queryFile = queryFileFlag
	}
	if querySuiteFileFlag != "" {
		querySuiteFile = querySuiteFileFlag
	}

	if controller == "" {
		fmt.Println("Please specify a controller.")
		os.Exit(1)
	}
	if listFile == "" {
		fmt.Println("Please specify a list file.")
		os.Exit(1)
	}
	if listName == "" {
		fmt.Println("Please specify a list name.")
		os.Exit(1)
	}
	if queryFile == "" && querySuiteFile == "" {
		fmt.Println("Please specify a query or query suite.")
		os.Exit(1)
	}

	if _, _, _, err := utils.LoadSession(sessionName); err == nil {
		fmt.Println("Session already exists.")
		os.Exit(1)
	}

	// read list of target repositories
	repositories, err := utils.ResolveRepositories(listFile, listName)
	if err != nil {
		log.Fatal(err)
	}

	// if a query suite is specified, resolve the queries
	queries := []string{}
	if queryFileFlag != "" {
		queries = append(queries, queryFileFlag)
	} else if querySuiteFileFlag != "" {
		queries = utils.ResolveQueries(codeqlPath, querySuiteFile)
	}

	fmt.Printf("Submitting %d queries for %d repositories\n", len(queries), len(repositories))
	var runs []models.Run
	for _, query := range queries {
		encodedBundle, err := utils.GenerateQueryPack(codeqlPath, query, language)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Generated encoded bundle for %s\n", query)

		var chunks [][]string
		for i := 0; i < len(repositories); i += config.MAX_MRVA_REPOSITORIES {
			end := i + config.MAX_MRVA_REPOSITORIES
			if end > len(repositories) {
				end = len(repositories)
			}
			chunks = append(chunks, repositories[i:end])
		}
		for _, chunk := range chunks {
			id, err := utils.SubmitRun(controller, language, chunk, encodedBundle)
			if err != nil {
				log.Fatal(err)
			}
			runs = append(runs, models.Run{Id: id, Query: query})
		}

	}
	if querySuiteFile != "" {
		err = utils.SaveSession(sessionName, controller, runs, language, listFile, listName, querySuiteFile, len(repositories))
	} else if queryFile != "" {
		err = utils.SaveSession(sessionName, controller, runs, language, listFile, listName, queryFile, len(repositories))
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Done!")
}
