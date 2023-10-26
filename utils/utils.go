package utils

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/GitHubSecurityLab/gh-mrva/models"
	"github.com/cli/go-gh"
	"github.com/cli/go-gh/pkg/api"
)

var (
	configFilePath   string
	sessionsFilePath string
)

func GetSessionsFilePath() string {
	return sessionsFilePath
}

func SetSessionsFilePath(path string) {
	sessionsFilePath = path
}

func GetConfigFilePath() string {
	return configFilePath
}

func SetConfigFilePath(path string) {
	configFilePath = path
}

func GetSessions() (map[string]models.Session, error) {
	sessionsFile, err := os.ReadFile(sessionsFilePath)
	var sessions map[string]models.Session
	if err != nil {
		return sessions, err
	}
	err = yaml.Unmarshal(sessionsFile, &sessions)
	if err != nil {
		log.Fatal(err)
	}
	return sessions, nil
}

func LoadRun(id int) (string, []models.Run, string, error) {
	sessions, err := GetSessions()
	if err != nil {
		return "", nil, "", err
	}
	if sessions != nil {
		for _, session := range sessions {
			for _, run := range session.Runs {
				if run.Id == id {
					return session.Controller, []models.Run{run}, session.Language, nil
				}
			}
		}
	}
	return "", nil, "", errors.New("No run found for " + fmt.Sprint(id))
}

func LoadSession(name string) (string, []models.Run, string, error) {
	sessions, err := GetSessions()
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

func GetSessionsStartingWith(prefix string) ([]string, error) {
	sessions, err := GetSessions()
	if err != nil {
		return nil, err
	}
	var matchingSessions []string
	if sessions != nil {
		for session := range sessions {
			if strings.HasPrefix(session, prefix) {
				matchingSessions = append(matchingSessions, session)
			}
		}
	}
	return matchingSessions, nil
}

func GetRunDetails(controller string, runId int) (map[string]interface{}, error) {
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

func GetRunRepositoryDetails(controller string, runId int, nwo string) (map[string]interface{}, error) {
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

func SaveSession(name string, controller string, runs []models.Run, language string, listFile string, list string, query string, count int) error {
	sessions, err := GetSessions()
	if err != nil {
		return err
	}
	if sessions == nil {
		sessions = make(map[string]models.Session)
	}
	// add new session if it doesn't already exist
	if _, ok := sessions[name]; ok {
		return errors.New(fmt.Sprintf("Session '%s' already exists", name))
	} else {
		sessions[name] = models.Session{
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
	err = os.WriteFile(sessionsFilePath, sessionsYaml, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func SubmitRun(controller string, language string, repoChunk []string, bundle string) (int, error) {
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

func GetConfig() (models.Config, error) {
	configFile, err := os.ReadFile(configFilePath)
	var configData models.Config
	if err != nil {
		return configData, err
	}
	err = yaml.Unmarshal(configFile, &configData)
	if err != nil {
		log.Fatal(err)
	}
	return configData, nil
}

func ResolveRepositories(listFile string, list string) ([]string, error) {
	fmt.Printf("Resolving %s repositories from %s\n", list, listFile)
	jsonFile, err := os.Open(listFile)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()
	byteValue, _ := io.ReadAll(jsonFile)
	var repoLists map[string][]string
	err = json.Unmarshal(byteValue, &repoLists)
	if err != nil {
		return nil, err
	}
	return repoLists[list], nil
}

func ResolveQueryId(queryFile string) (string, error) {
	queryId := ""
	args := []string{"resolve", "metadata", "--format=json", queryFile}
	fmt.Println("Resolving query id for", queryFile)
	jsonBytes, err := RunCodeQLCommand("", true, args...)
	fmt.Println("Metadata:", string(jsonBytes))
	var metadata map[string]interface{}
	if strings.TrimSpace(string(jsonBytes)) == "" {
		fmt.Println("No metadata found in the specified query file.")
		os.Exit(1)
	}
	err = json.Unmarshal(jsonBytes, &metadata)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if _, ok := metadata["id"]; ok {
		queryId = metadata["id"].(string)
		return queryId, nil
	} else {
		return "", errors.New("Failed to find query id in query file")
	}
}

func ResolveQueries(codeqlPath string, querySuite string) []string {
	args := []string{"resolve", "queries", "--format=json", querySuite}
	jsonBytes, err := RunCodeQLCommand(codeqlPath, false, args...)
	var queries []string
	if strings.TrimSpace(string(jsonBytes)) == "" {
		fmt.Println("No queries found in the specified query suite.")
		os.Exit(1)
	}
	err = json.Unmarshal(jsonBytes, &queries)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return queries
}

func RunCodeQLCommand(codeqlPath string, combined bool, args ...string) ([]byte, error) {
	if codeqlPath != "" && !strings.Contains(strings.Join(args, " "), "packlist") {
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

func GenerateQueryPack(codeqlPath string, queryFile string, language string) (string, string, error) {
	fmt.Printf("Generating query pack for %s\n", queryFile)

	// create a temporary directory to hold the query pack
	queryPackDir, err := os.MkdirTemp("", "query-pack-")
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
	queryId, err := ResolveQueryId(queryFile)
	if err != nil {
		log.Fatal(err)
	}
	originalPackRoot := FindPackRoot(queryFile)
	packRelativePath, _ := filepath.Rel(originalPackRoot, queryFile)
	targetQueryFileName := filepath.Join(queryPackDir, packRelativePath)

	if _, err := os.Stat(filepath.Join(originalPackRoot, "qlpack.yml")); errors.Is(err, os.ErrNotExist) {
		// qlpack.yml not found, generate a synthetic one
		fmt.Printf("QLPack does not exist. Generating synthetic one for %s\n", queryFile)
		// copy only the query file to the query pack directory
		err := CopyFile(queryFile, targetQueryFileName)
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
		toCopy := PackPacklist(codeqlPath, originalPackRoot, false)
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
			err := CopyFile(srcPath, targetPath)
			if err != nil {
				log.Fatal(err)
			}
		}
		fmt.Printf("Fixing QLPack in %s\n", queryPackDir)
		FixPackFile(queryPackDir, packRelativePath)
	}

	// assuming we are using 2.11.3 or later so Qlx remote is supported
	ccache := filepath.Join(originalPackRoot, ".cache")
	precompilationOpts := []string{"--qlx", "--no-default-compilation-cache", "--compilation-cache=" + ccache}
	bundlePath := filepath.Join(filepath.Dir(queryPackDir), fmt.Sprintf("qlpack-%s-generated.tgz", uuid.New().String()))

	// install the pack dependencies
	fmt.Print("Installing QLPack dependencies\n")
	args := []string{"pack", "install", queryPackDir}
	stdouterr, err := RunCodeQLCommand(codeqlPath, true, args...)
	if err != nil {
		fmt.Printf("`codeql pack bundle` failed with error: %v\n", string(stdouterr))
		return "", "", fmt.Errorf("Failed to install query pack: %v", err)
	}
	// bundle the query pack
	fmt.Print("Compiling and bundling the QLPack (This may take a while)\n")
	args = []string{"pack", "bundle", "-o", bundlePath, queryPackDir}
	args = append(args, precompilationOpts...)
	stdouterr, err = RunCodeQLCommand(codeqlPath, true, args...)
	if err != nil {
		fmt.Printf("`codeql pack bundle` failed with error: %v\n", string(stdouterr))
		return "", "", fmt.Errorf("Failed to bundle query pack: %v\n", err)
	}

	// open the bundle file and encode it as base64
	bundleFile, err := os.Open(bundlePath)
	if err != nil {
		return "", "", fmt.Errorf("Failed to open bundle file: %v\n", err)
	}
	defer bundleFile.Close()
	bundleBytes, err := io.ReadAll(bundleFile)
	if err != nil {
		return "", "", fmt.Errorf("Failed to read bundle file: %v\n", err)
	}
	bundleBase64 := base64.StdEncoding.EncodeToString(bundleBytes)

	return bundleBase64, queryId, nil
}

func PackPacklist(codeqlPath string, dir string, includeQueries bool) []string {
	// since 2.7.1, packlist returns an object with a "paths" property that is a list of packs.
	args := []string{"pack", "packlist", "--format=json"}
	if !includeQueries {
		args = append(args, "--no-include-queries")
	}
	args = append(args, dir)
	jsonBytes, err := RunCodeQLCommand(codeqlPath, false, args...)
	var packlist map[string][]string
	err = json.Unmarshal(jsonBytes, &packlist)
	if err != nil {
		log.Fatal(err)
	}
	return packlist["paths"]
}

func FindPackRoot(queryFile string) string {
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

func FixPackFile(queryPackDir string, packRelativePath string) error {
	packPath := filepath.Join(queryPackDir, "qlpack.yml")
	packFile, err := os.ReadFile(packPath)
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
	err = os.WriteFile(packPath, packFile, 0644)
	if err != nil {
		return err
	}
	return nil
}

func CopyFile(srcPath string, targetPath string) error {
	err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm)
	if err != nil {
		return err
	}
	bytesRead, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	err = os.WriteFile(targetPath, bytesRead, 0644)
	if err != nil {
		return err
	}
	return nil
}

func DownloadWorker(wg *sync.WaitGroup, taskChannel <-chan models.DownloadTask, resultChannel chan models.DownloadTask) {
	defer wg.Done()
	for task := range taskChannel {
		if task.Artifact == "artifact" {
			DownloadResults(task)
			resultChannel <- task
		} else if task.Artifact == "database" {
			fmt.Println("Downloading database", task.Nwo, task.Language, task.OutputDir, task.OutputFilename)
			DownloadDatabase(task)
			resultChannel <- task
		}
	}
}

func downloadArtifact(url string, task models.DownloadTask) error {
	client, err := gh.HTTPClient(nil)
	if err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		log.Fatal(err)
	}

	downloadedFiles := []string{}
	for _, zf := range zipReader.File {

		if zf.Name != "results.sarif" && zf.Name != "results.bqrs" {
			continue
		}
		f, err := zf.Open()
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		content, err := io.ReadAll(f)
		if err != nil {
			log.Fatal(err)
		}

		outputDir := task.OutputDir
		outputFilename := task.OutputFilename
		if zf.Name == "results.bqrs" {
			outputFilename = outputFilename + ".bqrs"
		} else if zf.Name == "results.sarif" {
			outputFilename = outputFilename + ".sarif"
		}

		// replace remote-query with real query id
		content = bytes.Replace(content, []byte("remote-query"), []byte(task.QueryId), -1)

		resultPath := filepath.Join(outputDir, outputFilename)
		err = os.WriteFile(resultPath, content, os.ModePerm)
		if err != nil {
			return err
		}
		downloadedFiles = append(downloadedFiles, resultPath)
	}

	if len(downloadedFiles) == 0 {
		return errors.New("No results files found in artifact")
	} else {
		fmt.Println("Downloaded", downloadedFiles)
		return nil
	}
}

func DownloadResults(task models.DownloadTask) error {
	// download artifact (BQRS or SARIF)
	runRepositoryDetails, err := GetRunRepositoryDetails(task.Controller, task.RunId, task.Nwo)
	if err != nil {
		return errors.New("Failed to get run repository details")
	}
	// download the results
	err = downloadArtifact(runRepositoryDetails["artifact_url"].(string), task)
	if err != nil {
		return errors.New("Failed to download artifact")
	}
	return nil
}

func DownloadDatabase(task models.DownloadTask) error {
	targetPath := filepath.Join(task.OutputDir, fmt.Sprintf("%s_db.zip", task.OutputFilename))
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/zip"},
	}
	client, err := gh.HTTPClient(&opts)
	if err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/code-scanning/codeql/databases/%s", task.Nwo, task.Language))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	err = os.WriteFile(targetPath, content, os.ModePerm)
	return nil
}
