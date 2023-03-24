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
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	MAX_MRVA_REPOSITORIES = 1000
)

var (
	configFilePath = ""
	controller     = ""
	language       = ""
	runName        = ""
	listFile       = ""
)

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

func resolveQueries(querySuite string) []string {
	args := []string{"resolve", "queries", "--format=json", querySuite}
	jsonBytes, err := exec.Command("codeql", args...).Output()
	var queries []string
	err = json.Unmarshal(jsonBytes, &queries)
	if err != nil {
		log.Fatal(err)
	}
	return queries
}

func packPacklist(dir string, includeQueries bool) []string {
	// since 2.7.1, packlist returns an object with a "paths" property that is a list of packs.
	args := []string{"pack", "packlist", "--format=json"}
	if !includeQueries {
		args = append(args, "--no-include-queries")
	}
	args = append(args, dir)
	jsonBytes, err := exec.Command("codeql", args...).Output()
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

// Fixes the qlpack.yml file to be correct in the context of the MRVA request.
// Performs the following fixes:
// - Updates the default suite of the query pack. This is used to ensure
// only the specified query is run.
// - Ensures the query pack name is set to the name expected by the server.
// - Removes any `${workspace}` version references from the qlpack.yml file. Converts them
// to `*` versions.
// @param queryPackDir The directory containing the query pack
// @param packRelativePath The relative path to the query pack from the root of the query pack
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
func generateQueryPack(queryFile string) (string, error) {
	fmt.Printf("Generating query pack for %s\n", queryFile)

	// create a temporary directory to hold the query pack
	queryPackDir, err := ioutil.TempDir("", "query-pack-")
	if err != nil {
		log.Fatal(err)
	}
	// TODO: uncomment this line when we're done debugging
	//defer os.RemoveAll(queryPackDir)

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
		toCopy := packPacklist(originalPackRoot, false)
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
	stdouterr, err := exec.Command("codeql", args...).CombinedOutput()
	if err != nil {
		fmt.Printf("`codeql pack bundle` failed with error: %v\n", string(stdouterr))
		return "", fmt.Errorf("Failed to install query pack: %v", err)
	}
	// bundle the query pack
	fmt.Print("Compiling and bundling the QLPack (This may take a while)\n")
	args = []string{"pack", "bundle", "-o", bundlePath, queryPackDir}
	args = append(args, precompilationOpts...)
	stdouterr, err = exec.Command("codeql", args...).CombinedOutput()
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
func submitRun(repoChunk []string, bundle string) (int, error) {
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

func getRunDetails(runId int) (map[string]interface{}, error) {
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

func getRunRepositoryDetails(runId int, nwo string) (map[string]interface{}, error) {
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

func downloadArtifact(url string, outputDir string, nwo string) (string, error) {
	client, err := gh.HTTPClient(nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
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
			return "", err
		}
		return resultPath, nil
	}
	return "", errors.New("No results.sarif file found in artifact")
}

func downloadDatabase(nwo string, lang string, targetPath string) error {
	opts := api.ClientOptions{
		Headers: map[string]string{"Accept": "application/zip"},
	}
	client, err := gh.HTTPClient(&opts)
	if err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/code-scanning/codeql/databases/%s", nwo, lang))
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

func saveInCache(name string, ids []int) error {
	configData, err := getConfig(configFilePath)
	if err != nil {
		return err
	}
	cache := configData.Cache
	if cache == nil {
		cache = map[string][]int{}
	}
	if cache[name] == nil {
		cache[name] = ids
	} else {
		cache[name] = append(cache[name], ids...)
	}
	// marshal config data to yaml
	configDataYaml, err := yaml.Marshal(configData)
	if err != nil {
		return err
	}
	// write config data to file
	err = ioutil.WriteFile(configFilePath, configDataYaml, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func loadFromCache(name string) ([]int, error) {
	configData, err := getConfig(configFilePath)
	if err != nil {
		return nil, err
	}
	if configData.Cache != nil {
		if configData.Cache[name] != nil {
			return configData.Cache[name], nil
		}
	}
	return []int{}, nil
}

func getConfig(path string) (Config, error) {
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

type Config struct {
	Controller string           `yaml:"controller"`
	ListFile   string           `yaml:"listFile"`
	Cache      map[string][]int `yaml:"cache"`
}

func main() {

	// read config file
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		homePath := os.Getenv("HOME")
		if homePath == "" {
			log.Fatal("HOME environment variable not set")
		}
		configPath = filepath.Join(homePath, ".config")
	}
	configFilePath = filepath.Join(configPath, "mrva", "config.yml")
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		// create config file if it doesn't exist
		// since we will use it for the name/ids cache
		err := os.MkdirAll(filepath.Dir(configFilePath), os.ModePerm)
		if err != nil {
			log.Println("Failed to create config file directory")
		}
		// create empty file at configFilePath
		configFile, err := os.Create(configFilePath)
		if err != nil {
			log.Fatal(err, "Failed to create config file")
		}
		configFile.Close()
	}
	configData, err := getConfig(configFilePath)
	if err != nil {
		log.Fatal(err)
	}
	if configData.Controller != "" {
		controller = configData.Controller
	}
	if configData.ListFile != "" {
		listFile = configData.ListFile
	}

	helpFlag := flag.String("help", "", "This help documentation.")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
gh mrva - submit and download CodeQL queries from MRVA

Usage:
	gh mrva submit --controller <controller> --lang <language> [--name <run name>] --list-file <list file> --list <list> --query <query>

	gh mrva download --run <run id> --lang <language> --controller <controller> --output-dir <output directory> [--name <run name>] [--download-dbs]

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
		submit(args)
	case "download":
		download(args)
	default:
		log.Fatalf("Unrecognized command %q. "+
			"Command must be one of: submit, download", cmd)
	}
}

func submit(args []string) {
	flag := flag.NewFlagSet("mrva submit", flag.ExitOnError)
	queryFileFlag := flag.String("query", "", "Path to query file")
	querySuiteFileFlag := flag.String("query-suite", "", "Path to query suite file")
	controllerFlag := flag.String("controller", "", "MRVA controller repository (overrides config file)")
	listFileFlag := flag.String("list-file", "", "Path to repo list file (overrides config file)")
	listFlag := flag.String("list", "", "Name of repo list")
	langFlag := flag.String("lang", "", "DB language")
	nameFlag := flag.String("name", "", "Name of run (optional)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
gh mrva - submit and download CodeQL queries from MRVA

Usage:
	gh mrva submit --controller <controller> --lang <language> [--name <run name>] --list-file <list file> --list <list> [--query <query> | --query-suite <query suite>]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	if *langFlag != "" {
		language = *langFlag
	}
	if *nameFlag != "" {
		runName = *nameFlag
	}
	if *controllerFlag != "" {
		controller = *controllerFlag
	}
	if *listFileFlag != "" {
		listFile = *listFileFlag
	}

	if controller == "" || language == "" || listFile == "" || *listFlag == "" || (*queryFileFlag == "" && *querySuiteFileFlag == "") {
		flag.Usage()
		os.Exit(1)
	}

	// read list of target repositories
	repositories, err := resolveRepositories(listFile, *listFlag)
	if err != nil {
		log.Fatal(err)
	}

	queries := []string{}
	if *queryFileFlag != "" {
		queries = append(queries, *queryFileFlag)
	} else if *querySuiteFileFlag != "" {
		queries = resolveQueries(*querySuiteFileFlag)
	}

	fmt.Printf("Requesting running %d queries for %d repositories\n", len(queries), len(repositories))

	for _, query := range queries {
		encodedBundle, err := generateQueryPack(query)
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
		var ids []int
		for _, chunk := range chunks {
			id, err := submitRun(chunk, encodedBundle)
			if err != nil {
				log.Fatal(err)
			}
			ids = append(ids, id)
		}
		fmt.Printf("Submitted run %v\n", ids)
		if runName != "" {
			err = saveInCache(runName, ids)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func download(args []string) {
	flag := flag.NewFlagSet("mrva submit", flag.ExitOnError)
	runFlag := flag.Int("run", 0, "MRVA run ID")
	outputDirFlag := flag.String("output-dir", "", "Output directory")
	downloadDBsFlag := flag.Bool("download-dbs", false, "Download databases (optional)")
	controllerFlag := flag.String("controller", "", "MRVA controller repository (overrides config file)")
	langFlag := flag.String("lang", "", "DB language")
	nameFlag := flag.String("name", "", "Name of run (optional)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
gh mrva - submit and download CodeQL queries from MRVA

Usage:
	gh mrva download --run <run id> --lang <language> --controller <controller> --output-dir <output directory> [--name <run name>] [--download-dbs]

`)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse(args)

	if *langFlag != "" {
		language = *langFlag
	}
	if *nameFlag != "" {
		runName = *nameFlag
	}
	if *controllerFlag != "" {
		controller = *controllerFlag
	}

	if controller == "" || language == "" || (*runFlag == 0 && runName == "") || *outputDirFlag == "" {
		flag.Usage()
		os.Exit(1)
	}

	// if outputDirFlag does not exist, create it
	if _, err := os.Stat(*outputDirFlag); os.IsNotExist(err) {
		err := os.MkdirAll(*outputDirFlag, os.ModePerm)
		if err != nil {
			log.Fatal(err)
		}
	}

	runIds := []int{}
	if *runFlag > 0 {
		runIds = []int{*runFlag}
	} else if runName != "" {
		ids, err := loadFromCache(runName)
		if err != nil {
			log.Fatal(err)
		}
		if len(ids) > 0 {
			runIds = ids
		}
	}

	for _, runId := range runIds {
		fmt.Printf("Downloading MRVA results for %s (%d)\n", controller, runId)
		// check if the run is complete
		runDetails, err := getRunDetails(runId)
		fmt.Printf("Status: %v\n", runDetails["status"])
		if err != nil {
			log.Fatal(err)
		}
		if runDetails["status"] == "in_progress" {
			log.Printf("Run %d is not complete yet. Please try again later.", runId)
			return
		}
		for _, r := range runDetails["scanned_repositories"].([]interface{}) {
			repo := r.(map[string]interface{})
			result_count := repo["result_count"]
			repoInfo := repo["repository"].(map[string]interface{})
			nwo := repoInfo["full_name"].(string)
			if result_count != nil && result_count.(float64) > 0 {
				fmt.Printf("Repo %s has %d results\n", nwo, int(result_count.(float64)))
				sarifPath := filepath.Join(*outputDirFlag, fmt.Sprintf("%s.sarif", strings.Replace(nwo, "/", "_", -1)))
				bqrsPath := filepath.Join(*outputDirFlag, fmt.Sprintf("%s.bqrs", strings.Replace(nwo, "/", "_", -1)))

				_, bqrsErr := os.Stat(bqrsPath)
				_, sarifErr := os.Stat(sarifPath)
				if errors.Is(bqrsErr, os.ErrNotExist) && errors.Is(sarifErr, os.ErrNotExist) {

					// download artifact (BQRS or SARIF)
					fmt.Printf("Downloading results for %s\n", repoInfo["full_name"])
					runRepositoryDetails, err := getRunRepositoryDetails(runId, nwo)
					if err != nil {
						log.Fatal(err)
					}
					// download the results
					artifactPath, err := downloadArtifact(runRepositoryDetails["artifact_url"].(string), *outputDirFlag, nwo)
					if err != nil {
						log.Fatal(err)
					}
					fmt.Printf("Artifact path: %s\n", artifactPath)
				}
				if *downloadDBsFlag {
					// download database
					targetPath := filepath.Join(*outputDirFlag, fmt.Sprintf("%s_%s_db.zip", strings.Replace(nwo, "/", "_", -1), language))
					if _, err := os.Stat(targetPath); errors.Is(err, os.ErrNotExist) {
						fmt.Printf("Downloading database for %s\n", nwo)
						err = downloadDatabase(nwo, language, targetPath)
						if err != nil {
							log.Fatal(err)
						}
						fmt.Printf("Database path: %s\n", targetPath)
					}
				}
			}
		}
	}
}
