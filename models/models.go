package models

import (
  "time"
)

type Run struct {
	Id      int    `yaml:"id"`
	Query   string `yaml:"query"`
	QueryId string `yaml:"query_id"`
}

type Session struct {
	Name            string    `yaml:"name" json:"name"`
	Timestamp       time.Time `yaml:"timestamp" json:"timestamp"`
	Runs            []Run     `yaml:"runs" json:"runs"`
	Controller      string    `yaml:"controller" json:"controller"`
	ListFile        string    `yaml:"list_file" json:"list_file"`
	List            string    `yaml:"list" json:"list"`
	Language        string    `yaml:"language" json:"language"`
	RepositoryCount int       `yaml:"repository_count" json:"repository_count"`
}

type Config struct {
	Controller string `yaml:"controller"`
	ListFile   string `yaml:"list_file"`
	CodeQLPath string `yaml:"codeql_path"`
}

type DownloadTask struct {
	RunId      int
	Nwo        string
	Controller string
	Artifact   string
	OutputDir  string
	Language   string
}

type RunStatus struct {
	Id            int    `json:"id"`
	Query         string `json:"query"`
	QueryId       string `json:"query_id"`
	Status        string `json:"status"`
	FailureReason string `json:"failure_reason"`
}

type RepoWithFindings struct {
	Query   string `json:"query"`
	QueryId string `json:"query_id"`
	Nwo     string `json:"nwo"`
	Count   int    `json:"count"`
	RunId   int    `json:"run_id"`
	Stars   int    `json:"stars"`
}

type Results struct {
	Name                                   string             `json:"name"`
  Status                                 string             `json:"status"`
  Runs                                   []RunStatus        `json:"runs"`
  ResositoriesWithFindings               []RepoWithFindings `json:"repositories_with_findings"`
  TotalFindingsCount                     int                `json:"total_findings_count"`
  TotalSuccessfulScans                   int                `json:"total_successful_scans"`
  TotalFailedScans                       int                `json:"total_failed_scans"`
  TotalRepositoriesWithFindings          int                `json:"total_repositories_with_findings"`
  TotalSkippedRepositories               int                `json:"total_skipped_repositories"`
  TotalSkippedAccessMismatchRepositories int                `json:"total_skipped_access_mismatch_repositories"`
  TotalSkippedNotFoundRepositories       int                `json:"total_skipped_not_found_repositories"`
  TotalSkippedNoDatabaseRepositories     int                `json:"total_skipped_no_database_repositories"`
  TotalSkippedOverLimitRepositories      int                `json:"total_skipped_over_limit_repositories"`
}

