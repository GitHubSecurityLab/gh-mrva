/*
Copyright © 2023 sessionNameFlag HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"

	"github.com/GitHubSecurityLab/gh-mrva/utils"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a saved session.",
	Long:  `Delete a saved session.`,
	Run: func(cmd *cobra.Command, args []string) {
		deleteSession()
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.Flags().StringVarP(&sessionNameFlag, "session", "s", "", "Session name be deleted")
	deleteCmd.MarkFlagRequired("session")
}

func deleteSession() error {
	sessions, err := utils.GetSessions()
	if err != nil {
		return err
	}
	if sessions == nil {
		return errors.New("No sessions found")
	}
	// delete session if it exists
	if _, ok := sessions[sessionNameFlag]; ok {

		delete(sessions, sessionNameFlag)

		// marshal sessions to yaml
		sessionsYaml, err := yaml.Marshal(sessions)
		if err != nil {
			return err
		}
		// write sessions to file
		err = ioutil.WriteFile(utils.GetSessionsFilePath(), sessionsYaml, 0755)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New(fmt.Sprintf("Session '%s' does not exist", sessionNameFlag))
}
