/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/GitHubSecurityLab/gh-mrva/models"
	"github.com/GitHubSecurityLab/gh-mrva/utils"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved sessions.",
	Long:  `List saved sessions.`,
	Run: func(cmd *cobra.Command, args []string) {
		listSessions()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format (default: false)")
}

func listSessions() {
	sessions, err := utils.GetSessions()
	if err != nil {
		log.Fatal(err)
	}
	if sessions != nil {
		if jsonFlag {
			sessions_list := make([]models.Session, 0, len(sessions))
			for _, session := range sessions {
				sessions_list = append(sessions_list, session)
			}
			data, err := json.MarshalIndent(sessions_list, "", "  ")
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(data))
			// w := &bytes.Buffer{}
			// jsonpretty.Format(w, bytes.NewReader(data), "  ", true)
			// fmt.Println(w.String())
		} else {
			for name, entry := range sessions {
				fmt.Printf("%s (%v)\n", name, entry.Timestamp)
				fmt.Printf("  Controller: %s\n", entry.Controller)
				fmt.Printf("  Language: %s\n", entry.Language)
				fmt.Printf("  List file: %s\n", entry.ListFile)
				fmt.Printf("  List: %s\n", entry.List)
				fmt.Printf("  Repository count: %d\n", entry.RepositoryCount)
				fmt.Println("  Runs:")
				for _, run := range entry.Runs {
					fmt.Printf("    ID: %d\n", run.Id)
					fmt.Printf("    Query: %s\n", run.Query)
				}
			}
		}
	}
}
