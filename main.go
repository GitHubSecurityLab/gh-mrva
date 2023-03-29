package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/cli/go-gh"
	"github.com/cli/go-gh/pkg/api"
	// "github.com/cli/go-gh/pkg/jsonpretty"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"
)

const (
	MAX_MRVA_REPOSITORIES = 1000
	WORKERS               = 10
)

var (
	configFilePath   string
	sessionsFilePath string
)

func runCodeQLCommand(codeqlPath string, combined bool, args ...string) ([]byte, error) {
	if !strings.Contains(strings.Join(args, " "), "packlist") {
		args = append(args, fmt.Sprintf("--additional-packs=%s", codeqlPath))
	}
	cmd := exec.Command("codeql", args...)
	cmd.Env = os.Environ()
	if combined {
		return cmd.CombinedOutput()
	} else {
		return cmd.Output()
	}
}
func resolveRepositories(listFile string, list string) ([]string, error) {
	fmt.Printf("Resolving %s repositories from %s\n", list, listFile)
	jsonFile, err := os.Open(listFile)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	var repoLists map[string][]string
	err = json.Unmarshal(byteValue, &repoLists)
	if err != nil {
		log.Fatal(err)
	}
	return repoLists[list], nil
}

func resolveQueries(codeqlPath string, querySuite string) []string {
	args := []string{"resolve", "queries", "--format=json", querySuite}
	jsonBytes, err := runCodeQLCommand(codeqlPath, false, args...)
	var queries []string
	err = json.Unmarshal(jsonBytes, &queries)
	if err != nil {
		log.Fatal(err)
	}
	return queries
}

func packPacklist(codeqlPath string, dir string, includeQueries bool) []string {
	// since 2.7.1, packlist returns an object with a "paths" property that is a list of packs.
	args := []string{"pack", "packlist", "--format=json"}
	if !includeQueries {
		args = append(args, "--no-include-queries")
	}
	args = append(args, dir)
	jsonBytes, err := runCodeQLCommand(codeqlPath, false, args...)
	var packlist map[string][]string
	err = json.Unmarshal(jsonBytes, &packlist)
	if err != nil {
		log.Fatal(err)
	}
	return packlist["paths"]
}

func findPackRoot(queryFile string) string {
	// Starting on the directory of queryPackDir, go down until a qlpack.yml find is found. return that directory
	// If no qlpack.yml is found, return the directory of queryFile
	currentDir := filepath.Dir(queryFile)
	for currentDir != "/" {
		if _, err := os.Stat(filepath.Join(currentDir, "qlpack.yml")); errors.Is(err, os.ErrNotExist) {
			// qlpack.yml not found, go up one level
			currentDir = filepath.Dir(currentDir)
		} else {
			return currentDir
		}
	}
	return filepath.Dir(queryFile)
}

func copyFile(srcPath string, targetPath string) error {
	err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm)
	if err != nil {
		return err
	}
	bytesRead, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(targetPath, bytesRead, 0644)
	if err != nil {
		return err
	}
	return nil
}

func fixPackFile(queryPackDir string, packRelativePath string) error {
	packPath := filepath.Join(queryPackDir, "qlpack.yml")
	packFile, err := ioutil.ReadFile(packPath)
	if err != nil {
		return err
	}
	var packData map[string]interface{}
	err = yaml.Unmarshal(packFile, &packData)
	if err != nil {
		return err
	}
	// update the default suite
	defaultSuiteFile := packData["defaultSuiteFile"]
	if defaultSuiteFile != nil {
		// remove the defaultSuiteFile property
		delete(packData, "defaultSuiteFile")
	}
	packData["defaultSuite"] = map[string]string{
		"query":       packRelativePath,
		"description": "Query suite for Variant Analysis",
	}

	// update the name
	packData["name"] = "codeql-remote/query"

	// remove any `${workspace}` version references
	dependencies := packData["dependencies"]
	if dependencies != nil {
		// for key and value in dependencies
		for key, value := range dependencies.(map[string]interface{}) {
			// if value is a string and value contains `${workspace}`
			if value == "${workspace}" {
				// replace the value with `*`
				packData["dependencies"].(map[string]interface{})[key] = "*"
			}
		}
	}

	// write the pack file
	packFile, err = yaml.Marshal(packData)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(packPath, packFile, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Generate a query pack containing the given query file.
func generateQueryPack(codeqlPath string, queryFile string, language string) (string, error) {
	fmt.Printf("Generating query pack for %s\n", queryFile)

	// create a temporary directory to hold the query pack
	queryPackDir, err := ioutil.TempDir("", "query-pack-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(queryPackDir)

	queryFile, err = filepath.Abs(queryFile)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stat(queryFile); errors.Is(err, os.ErrNotExist) {
		log.Fatal(fmt.Sprintf("Query file %s does not exist", queryFile))
	}
	originalPackRoot := findPackRoot(queryFile)
	packRelativePath, _ := filepath.Rel(originalPackRoot, queryFile)
	targetQueryFileName := filepath.Join(queryPackDir, packRelativePath)

	if _, err := os.Stat(filepath.Join(originalPackRoot, "qlpack.yml")); errors.Is(err, os.ErrNotExist) {
		// qlpack.yml not found, generate a synthetic one
		fmt.Printf("QLPack does not exist. Generating synthetic one for %s\n", queryFile)
		// copy only the query file to the query pack directory
		err := copyFile(queryFile, targetQueryFileName)
		if err != nil {
			log.Fatal(err)
		}
		// generate a synthetic qlpack.yml
		td := struct {
			Language string
			Name     string
			Query    string
		}{
			Language: language,
			Name:     "codeql-remote/query",
			Query:    strings.Replace(packRelativePath, string(os.PathSeparator), "/", -1),
		}
		t, err := template.New("").Parse(`name: {{ .Name }}
version: 0.0.0
dependencies:
  codeql/{{ .Language }}-all: "*"
defaultSuite:
  description: Query suite for variant analysis
  query: {{ .Query }}`)
		if err != nil {
			log.Fatal(err)
		}

		f, err := os.Create(filepath.Join(queryPackDir, "qlpack.yml"))
		defer f.Close()
		if err != nil {
			log.Fatal(err)
		}
		err = t.Execute(f, td)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Copied QLPack files to %s\n", queryPackDir)
	} else {
		// don't include all query files in the QLPacks. We only want the queryFile to be copied.
		fmt.Printf("QLPack exists, stripping all other queries from %s\n", originalPackRoot)
		toCopy := packPacklist(codeqlPath, originalPackRoot, false)
		// also copy the lock file (either new name or old name) and the query file itself (these are not included in the packlist)
		lockFileNew := filepath.Join(originalPackRoot, "qlpack.lock.yml")
		lockFileOld := filepath.Join(originalPackRoot, "codeql-pack.lock.yml")
		candidateFiles := []string{lockFileNew, lockFileOld, queryFile}
		for _, candidateFile := range candidateFiles {
			if _, err := os.Stat(candidateFile); !errors.Is(err, os.ErrNotExist) {
				// if the file exists, copy it
				toCopy = append(toCopy, candidateFile)
			}
		}
		// copy the files to the queryPackDir directory
		fmt.Printf("Preparing stripped QLPack in %s\n", queryPackDir)
		for _, srcPath := range toCopy {
			relPath, _ := filepath.Rel(originalPackRoot, srcPath)
			targetPath := filepath.Join(queryPackDir, relPath)
			//fmt.Printf("Copying %s to %s\n", srcPath, targetPath)
			err := copyFile(srcPath, targetPath)
			if err != nil {
				log.Fatal(err)
			}
		}
		fmt.Printf("Fixing QLPack in %s\n", queryPackDir)
		fixPackFile(queryPackDir, packRelativePath)
	}

	// assuming we are using 2.11.3 or later so Qlx remote is supported
	ccache := filepath.Join(originalPackRoot, ".cache")
	precompilationOpts := []string{"--qlx", "--no-default-compilation-cache", "--compilation-cache=" + ccache}
	bundlePath := filepath.Join(filepath.Dir(queryPackDir), fmt.Sprintf("qlpack-%s-generated.tgz", uuid.New().String()))

	// install the pack dependencies
	fmt.Print("Installing QLPack dependencies\n")
	args := []string{"pack", "install", queryPackDir}
	stdouterr, err := runCodeQLCommand(codeqlPath, true, args...)
	if err != nil {
		fmt.Printf("`codeql pack bundle` failed with error: %v\n", string(stdouterr))
		return "", fmt.Errorf("Failed to install query pack: %v", err)
	}
	// bundle the query pack
	fmt.Print("Compiling and bundling the QLPack (This may take a while)\n")
	args = []string{"pack", "bundle", "-o", bundlePath, queryPackDir}
	args = append(args, precompilationOpts...)
	stdouterr, err = runCodeQLCommand(codeqlPath, true, args...)
	if err != nil {
		fmt.Printf("`codeql pack bundle` failed with error: %v\n", string(stdouterr))
		return "", fmt.Errorf("Failed to bundle query pack: %v\n", err)
	}

	// open the bundle file and encode it as base64
	bundleFile, err := os.Open(bundlePath)
	if err != nil {
		return "", fmt.Errorf("Failed to open bundle file: %v\n", err)
	}
	defer bundleFile.Close()
	bundleBytes, err := ioutil.ReadAll(bundleFile)
	if err != nil {
		return "", fmt.Errorf("Failed to read bundle file: %v\n", err)
	}
	bundleBase64 := base64.StdEncoding.EncodeToString(bundleBytes)

	return bundleBase64, nil
}

// Requests a query to be run against `respositories` on the given `controller`.
func submitRun(controller string, language string, repoChunk []string, bundle string) (int, error) {
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/vnd.github.v3+json"},
	}
	client, err := gh.RESTClient(&opts)
	if err != nil {
		return -1, err
	}
	body := struct {
		Repositories []string `json:"repositories"`
		Language     string   `json:"language"`
		Pack         string   `json:"query_pack"`
		Ref          string   `json:"action_repo_ref"`
	}{
		Repositories: repoChunk,
		Language:     language,
		Pack:         bundle,
		Ref:          "main",
	}
	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(body)
	if err != nil {
		return -1, err
	}
	response := make(map[string]interface{})
	err = client.Post(fmt.Sprintf("repos/%s/code-scanning/codeql/variant-analyses", controller), &buf, &response)
	if err != nil {
		return -1, err
	}
	id := int(response["id"].(float64))
	return id, nil
}

func getRunDetails(controller string, runId int) (map[string]interface{}, error) {
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/vnd.github.v3+json"},
	}
	client, err := gh.RESTClient(&opts)
	if err != nil {
		return nil, err
	}
	response := make(map[string]interface{})
	err = client.Get(fmt.Sprintf("repos/%s/code-scanning/codeql/variant-analyses/%d", controller, runId), &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func getRunRepositoryDetails(controller string, runId int, nwo string) (map[string]interface{}, error) {
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/vnd.github.v3+json"},
	}
	client, err := gh.RESTClient(&opts)
	if err != nil {
		return nil, err
	}
	response := make(map[string]interface{})
	err = client.Get(fmt.Sprintf("repos/%s/code-scanning/codeql/variant-analyses/%d/repos/%s", controller, runId, nwo), &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

type DownloadTask struct {
	runId      int
	nwo        string
	controller string
	artifact   string
	outputDir  string
	language   string
}

func downloadWorker(wg *sync.WaitGroup, taskChannel <-chan DownloadTask, resultChannel chan DownloadTask) {
	defer wg.Done()
	for task := range taskChannel {
		if task.artifact == "artifact" {
			downloadResults(task.controller, task.runId, task.nwo, task.outputDir)
			resultChannel <- task
		} else if task.artifact == "database" {
			fmt.Println("Downloading database", task.nwo, task.language, task.outputDir)
			downloadDatabase(task.nwo, task.language, task.outputDir)
			resultChannel <- task
		}
	}
}

func downloadArtifact(url string, outputDir string, nwo string) error {
	client, err := gh.HTTPClient(nil)
	if err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		log.Fatal(err)
	}

	for _, zf := range zipReader.File {
		if zf.Name != "results.sarif" && zf.Name != "results.bqrs" {
			continue
		}
		f, err := zf.Open()
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		bytes, err := ioutil.ReadAll(f)
		if err != nil {
			log.Fatal(err)
		}
		extension := ""
		resultPath := ""
		if zf.Name == "results.bqrs" {
			extension = "bqrs"
		} else if zf.Name == "results.sarif" {
			extension = "sarif"
		}
		resultPath = filepath.Join(outputDir, fmt.Sprintf("%s.%s", strings.Replace(nwo, "/", "_", -1), extension))
		err = ioutil.WriteFile(resultPath, bytes, os.ModePerm)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("No results.sarif file found in artifact")
}

func downloadResults(controller string, runId int, nwo string, outputDir string) error {
	// download artifact (BQRS or SARIF)
	runRepositoryDetails, err := getRunRepositoryDetails(controller, runId, nwo)
	if err != nil {
		return errors.New("Failed to get run repository details")
	}
	// download the results
	err = downloadArtifact(runRepositoryDetails["artifact_url"].(string), outputDir, nwo)
	if err != nil {
		return errors.New("Failed to download artifact")
	}
	return nil
}

func downloadDatabase(nwo string, language string, outputDir string) error {
	dnwo := strings.Replace(nwo, "/", "_", -1)
	targetPath := filepath.Join(outputDir, fmt.Sprintf("%s_%s_db.zip", dnwo, language))
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/zip"},
	}
	client, err := gh.HTTPClient(&opts)
	if err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/code-scanning/codeql/databases/%s", nwo, language))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(targetPath, bytes, os.ModePerm)
	return nil
}

func saveSession(name string, controller string, runs []Run, language string, listFile string, list string, query string, count int) error {
	sessions, err := getSessions()
	if err != nil {
		return err
	}
	if sessions == nil {
		sessions = make(map[string]Session)
	}
	// add new session if it doesn't already exist
	if _, ok := sessions[name]; ok {
		return errors.New(fmt.Sprintf("Session '%s' already exists", name))
	} else {
		sessions[name] = Session{
			Name:            name,
			Runs:            runs,
			Timestamp:       time.Now(),
			Controller:      controller,
			Language:        language,
			ListFile:        listFile,
			List:            list,
			RepositoryCount: count,
		}
	}
	// marshal sessions to yaml
	sessionsYaml, err := yaml.Marshal(sessions)
	if err != nil {
		return err
	}
	// write sessions to file
	err = ioutil.WriteFile(sessionsFilePath, sessionsYaml, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func loadSession(name string) (string, []Run, string, error) {
	sessions, err := getSessions()
	if err != nil {
		return "", nil, "", err
	}
	if sessions != nil {
		if entry, ok := sessions[name]; ok {
			return entry.Controller, entry.Runs, entry.Language, nil
		}
	}
	return "", nil, "", errors.New("No session found for " + name)
}

func getSessions() (map[string]Session, error) {
	sessionsFile, err := ioutil.ReadFile(sessionsFilePath)
	var sessions map[string]Session
	if err != nil {
		return sessions, err
	}
	err = yaml.Unmarshal(sessionsFile, &sessions)
	if err != nil {
		log.Fatal(err)
	}
	return sessions, nil
}

func getConfig() (Config, error) {
	configFile, err := ioutil.ReadFile(configFilePath)
	var configData Config
	if err != nil {
		return configData, err
	}
	err = yaml.Unmarshal(configFile, &configData)
	if err != nil {
		log.Fatal(err)
	}
	return configData, nil
}

type Run struct {
	Id    int    `yaml:"id"`
	Query string `yaml:"query"`
}

type Session struct {
	Name            string    `yaml:"name"`
	Timestamp       time.Time `yaml:"timestamp"`
	Runs            []Run     `yaml:"runs"`
	Controller      string    `yaml:"controller"`
	ListFile        string    `yaml:"list_file"`
	List            string    `yaml:"list"`
	Language        string    `yaml:"language"`
	RepositoryCount int       `yaml:"repository_count"`
}
type Config struct {
	Controller string `yaml:"controller"`
	ListFile   string `yaml:"list_file"`
	CodeQLPath string `yaml:"codeql_path"`
}

func main() {
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		homePath := os.Getenv("HOME")
		if homePath == "" {
			log.Fatal("HOME environment variable not set")
		}
		configPath = filepath.Join(homePath, ".config")
	}
	configFilePath = filepath.Join(configPath, "gh-mrva", "config.yml")
	configData, err := getConfig()
	if err != nil {
		log.Fatal(err)
	}

	sessionsFilePath = filepath.Join(configPath, "gh-mrva", "sessions.yml")
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
	helpFlag := flag.String("help", "", "This help documentation.")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gh mrva - Run CodeQL queries at scale using Multi-Repository Variant Analysis (MRVA)

Usage:
  gh mrva submit [--codeql-path <path to CodeQL>] [--controller <controller>] --lang <language> --name <run name> [--list-file <list file>] --list <list> [--query <query> | --query-suite <query suite>]

	gh mrva download --name <run name> --output-dir <output directory> [--download-dbs] [--nwo <owner/repo>]

  gh mrva status --name <run name> [--json]

  gh mrva list [--json]
`)
	}

	flag.Parse()

	if *helpFlag != "" {
		flag.Usage()
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(0)
	}
	cmd, args := args[0], args[1:]

	switch cmd {
	case "submit":
		submit(configData, args)
	case "download":
		download(args)
	case "status":
		status(args)
	case "list":
		list(args)
	default:
		log.Fatalf("Unrecognized command %q. "+
			"Command must be one of: submit, download", cmd)
	}
}

func status(args []string) {
	flag := flag.NewFlagSet("mrva status", flag.ExitOnError)
	nameFlag := flag.String("name", "", "Name of run")
	jsonFlag := flag.Bool("json", false, "Output in JSON format (default: false)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gh mrva - Run CodeQL queries at scale using Multi-Repository Variant Analysis (MRVA)

Usage:
  gh mrva status --name <run name> [--json]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	var (
		runName    = *nameFlag
		jsonOutput = *jsonFlag
	)

	if runName == "" {
		flag.Usage()
		os.Exit(1)
	}

	controller, runs, _, err := loadSession(runName)
	if err != nil {
		log.Fatal(err)
	}
	if len(runs) == 0 {
		log.Fatal("No runs found for run name", runName)
	}

	type RunStatus struct {
		Id            int    `json:"id"`
		Query         string `json:"query"`
		Status        string `json:"status"`
		FailureReason string `json:"failure_reason"`
	}

	type RepoWithFindings struct {
		Nwo   string `json:"nwo"`
		Count int    `json:"count"`
		RunId int    `json:"run_id"`
	}
	type Results struct {
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

	var results Results

	for _, run := range runs {
		if err != nil {
			log.Fatal(err)
		}
		runDetails, err := getRunDetails(controller, run.Id)
		if err != nil {
			log.Fatal(err)
		}

		status := runDetails["status"].(string)
		var failure_reason string
		if status == "failed" {
			failure_reason = runDetails["failure_reason"].(string)
		} else {
			failure_reason = ""
		}

		results.Runs = append(results.Runs, RunStatus{
			Id:            run.Id,
			Query:         run.Query,
			Status:        status,
			FailureReason: failure_reason,
		})

		for _, repo := range runDetails["scanned_repositories"].([]interface{}) {
			if repo.(map[string]interface{})["analysis_status"].(string) == "succeeded" {
				results.TotalSuccessfulScans += 1
				if repo.(map[string]interface{})["result_count"].(float64) > 0 {
					results.TotalRepositoriesWithFindings += 1
					results.TotalFindingsCount += int(repo.(map[string]interface{})["result_count"].(float64))
					repoInfo := repo.(map[string]interface{})["repository"].(map[string]interface{})
					results.ResositoriesWithFindings = append(results.ResositoriesWithFindings, RepoWithFindings{
						Nwo:   repoInfo["full_name"].(string),
						Count: int(repo.(map[string]interface{})["result_count"].(float64)),
						RunId: run.Id,
					})
				}
			} else if repo.(map[string]interface{})["analysis_status"].(string) == "failed" {
				results.TotalFailedScans += 1
			}
		}

		skipped_repositories := runDetails["skipped_repositories"].(map[string]interface{})
		access_mismatch_repos := skipped_repositories["access_mismatch_repos"].(map[string]interface{})
		not_found_repos := skipped_repositories["not_found_repos"].(map[string]interface{})
		no_codeql_db_repos := skipped_repositories["no_codeql_db_repos"].(map[string]interface{})
		over_limit_repos := skipped_repositories["over_limit_repos"].(map[string]interface{})
		total_skipped_repos := access_mismatch_repos["repository_count"].(float64) + not_found_repos["repository_count"].(float64) + no_codeql_db_repos["repository_count"].(float64) + over_limit_repos["repository_count"].(float64)

		results.TotalSkippedAccessMismatchRepositories += int(access_mismatch_repos["repository_count"].(float64))
		results.TotalSkippedNotFoundRepositories += int(not_found_repos["repository_count"].(float64))
		results.TotalSkippedNoDatabaseRepositories += int(no_codeql_db_repos["repository_count"].(float64))
		results.TotalSkippedOverLimitRepositories += int(over_limit_repos["repository_count"].(float64))
		results.TotalSkippedRepositories += int(total_skipped_repos)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(data))
		// w := &bytes.Buffer{}
		// jsonpretty.Format(w, bytes.NewReader(data), "  ", true)
		// fmt.Println(w.String())
	} else {
		// Print results in a nice way
		fmt.Println("Run name:", runName)
		fmt.Println("Total runs:", len(results.Runs))
		fmt.Println("Total successful scans:", results.TotalSuccessfulScans)
		fmt.Println("Total failed scans:", results.TotalFailedScans)
		fmt.Println("Total skipped repositories:", results.TotalSkippedRepositories)
		fmt.Println("Total skipped repositories due to access mismatch:", results.TotalSkippedAccessMismatchRepositories)
		fmt.Println("Total skipped repositories due to not found:", results.TotalSkippedNotFoundRepositories)
		fmt.Println("Total skipped repositories due to no database:", results.TotalSkippedNoDatabaseRepositories)
		fmt.Println("Total skipped repositories due to over limit:", results.TotalSkippedOverLimitRepositories)
		fmt.Println("Total repositories with findings:", results.TotalRepositoriesWithFindings)
		fmt.Println("Total findings:", results.TotalFindingsCount)
		fmt.Println("Repositories with findings:")
		for _, repo := range results.ResositoriesWithFindings {
			fmt.Println("  ", repo.Nwo, ":", repo.Count)
		}
	}
}

func submit(configData Config, args []string) {

	flag := flag.NewFlagSet("mrva submit", flag.ExitOnError)
	queryFileFlag := flag.String("query", "", "Path to query file")
	querySuiteFileFlag := flag.String("query-suite", "", "Path to query suite file")
	controllerFlag := flag.String("controller", "", "MRVA controller repository (overrides config file)")
	codeqlPathFlag := flag.String("codeql-path", "", "Path to CodeQL distribution (overrides config file)")
	listFileFlag := flag.String("list-file", "", "Path to repo list file (overrides config file)")
	listFlag := flag.String("list", "", "Name of repo list")
	langFlag := flag.String("lang", "", "DB language")
	nameFlag := flag.String("name", "", "Name of run")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gh mrva - Run CodeQL queries at scale using Multi-Repository Variant Analysis (MRVA)

Usage:
	gh mrva submit [--codeql-path <path to CodeQL>] [--controller <controller>] --lang <language> --name <run name> [--list-file <list file>] --list <list> [--query <query> | --query-suite <query suite>]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	var (
		controller     string
		codeqlPath     string
		listFile       string
		list           string
		language       string
		runName        string
		queryFile      string
		querySuiteFile string
	)
	if *controllerFlag != "" {
		controller = *controllerFlag
	} else if configData.Controller != "" {
		controller = configData.Controller
	}
	if *listFileFlag != "" {
		listFile = *listFileFlag
	} else if configData.ListFile != "" {
		listFile = configData.ListFile
	}
	if *codeqlPathFlag != "" {
		codeqlPath = *codeqlPathFlag
	} else if configData.CodeQLPath != "" {
		codeqlPath = configData.CodeQLPath
	}
	if *langFlag != "" {
		language = *langFlag
	}
	if *nameFlag != "" {
		runName = *nameFlag
	}
	if *listFlag != "" {
		list = *listFlag
	}
	if *queryFileFlag != "" {
		queryFile = *queryFileFlag
	}
	if *querySuiteFileFlag != "" {
		querySuiteFile = *querySuiteFileFlag
	}

	if runName == "" || codeqlPath == "" || controller == "" || language == "" || listFile == "" || list == "" || (queryFile == "" && querySuiteFile == "") {
		flag.Usage()
		os.Exit(1)
	}

	// read list of target repositories
	repositories, err := resolveRepositories(listFile, list)
	if err != nil {
		log.Fatal(err)
	}

	// if a query suite is specified, resolve the queries
	queries := []string{}
	if *queryFileFlag != "" {
		queries = append(queries, *queryFileFlag)
	} else if *querySuiteFileFlag != "" {
		queries = resolveQueries(codeqlPath, querySuiteFile)
	}

	fmt.Printf("Submitting %d queries for %d repositories\n", len(queries), len(repositories))
	var runs []Run
	for _, query := range queries {
		encodedBundle, err := generateQueryPack(codeqlPath, query, language)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Generated encoded bundle for %s\n", query)

		var chunks [][]string
		for i := 0; i < len(repositories); i += MAX_MRVA_REPOSITORIES {
			end := i + MAX_MRVA_REPOSITORIES
			if end > len(repositories) {
				end = len(repositories)
			}
			chunks = append(chunks, repositories[i:end])
		}
		for _, chunk := range chunks {
			id, err := submitRun(controller, language, chunk, encodedBundle)
			if err != nil {
				log.Fatal(err)
			}
			runs = append(runs, Run{Id: id, Query: query})
		}

	}
	if querySuiteFile != "" {
		err = saveSession(runName, controller, runs, language, listFile, list, querySuiteFile, len(repositories))
	} else if queryFile != "" {
		err = saveSession(runName, controller, runs, language, listFile, list, queryFile, len(repositories))
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Done!")
}

func list(args []string) {
	flag := flag.NewFlagSet("mrva list", flag.ExitOnError)
	jsonFlag := flag.Bool("json", false, "Output in JSON format (default: false)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gh mrva - Run CodeQL queries at scale using Multi-Repository Variant Analysis (MRVA)

Usage:
	gh mrva list [--json]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	var jsonOutput = *jsonFlag

	sessions, err := getSessions()
	if err != nil {
		log.Fatal(err)
	}
	if sessions != nil {
		if jsonOutput {
			for _, entry := range sessions {
				data, err := json.MarshalIndent(entry, "", "  ")
				if err != nil {
					log.Fatal(err)
				}
				fmt.Println(string(data))
				// w := &bytes.Buffer{}
				// jsonpretty.Format(w, bytes.NewReader(data), "  ", true)
				// fmt.Println(w.String())
			}
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

func download(args []string) {
	flag := flag.NewFlagSet("mrva download", flag.ExitOnError)
	nameFlag := flag.String("name", "", "Name of run")
	outputDirFlag := flag.String("output-dir", "", "Output directory")
	downloadDBsFlag := flag.Bool("download-dbs", false, "Download databases (optional)")
	nwoFlag := flag.String("nwo", "", "Repository to download artifacts for (optional)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gh mrva - Run CodeQL queries at scale using Multi-Repository Variant Analysis (MRVA)

Usage:
	gh mrva download --name <run name> --output-dir <output directory> [--download-dbs] [--nwo <owner/repo>]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	var (
		runName     = *nameFlag
		outputDir   = *outputDirFlag
		downloadDBs = *downloadDBsFlag
		targetNwo   = *nwoFlag
	)

	if runName == "" || outputDir == "" {
		flag.Usage()
		os.Exit(1)
	}

	// if outputDir does not exist, create it
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		err := os.MkdirAll(outputDir, os.ModePerm)
		if err != nil {
			log.Fatal(err)
		}
	}

	controller, runs, language, err := loadSession(runName)
	if err != nil {
		log.Fatal(err)
	} else if len(runs) == 0 {
		log.Fatal("No runs found for name " + runName)
	}

	var downloadTasks []DownloadTask

	for _, run := range runs {
		runDetails, err := getRunDetails(controller, run.Id)
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
			// if targetNwo is set, only download artifacts for that repository
			if targetNwo != "" && targetNwo != nwo {
				continue
			}
			if result_count != nil && result_count.(float64) > 0 {
				// check if the SARIF or BQRS file already exists
				dnwo := strings.Replace(nwo, "/", "_", -1)
				sarifPath := filepath.Join(outputDir, fmt.Sprintf("%s.sarif", dnwo))
				bqrsPath := filepath.Join(outputDir, fmt.Sprintf("%s.bqrs", dnwo))
				targetPath := filepath.Join(outputDir, fmt.Sprintf("%s_%s_db.zip", dnwo, language))
				_, bqrsErr := os.Stat(bqrsPath)
				_, sarifErr := os.Stat(sarifPath)
				if errors.Is(bqrsErr, os.ErrNotExist) && errors.Is(sarifErr, os.ErrNotExist) {
					downloadTasks = append(downloadTasks, DownloadTask{
						runId:      run.Id,
						nwo:        nwo,
						controller: controller,
						artifact:   "artifact",
						language:   language,
						outputDir:  outputDir,
					})
				}
				if downloadDBs {
					// check if the database already exists
					if _, err := os.Stat(targetPath); errors.Is(err, os.ErrNotExist) {
						downloadTasks = append(downloadTasks, DownloadTask{
							runId:      run.Id,
							nwo:        nwo,
							controller: controller,
							artifact:   "database",
							language:   language,
							outputDir:  outputDir,
						})
					}
				}
			}
		}
	}

	wg := new(sync.WaitGroup)

	taskChannel := make(chan DownloadTask)
	resultChannel := make(chan DownloadTask, len(downloadTasks))

	// Start the workers
	for i := 0; i < WORKERS; i++ {
		wg.Add(1)
		go downloadWorker(wg, taskChannel, resultChannel)
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
			fmt.Printf("Downloaded %s for %s (%d/%d)\n", value.artifact, value.nwo, count, len(downloadTasks))
		}
		fmt.Println(count, " artifacts downloaded")
		progressDone <- true
	}()

	// wait for all workers to finish
	wg.Wait()

	// close the result channel
	close(resultChannel)

	// drain the progress channel
	<-progressDone
}
