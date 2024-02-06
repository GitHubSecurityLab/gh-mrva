- [Using MRVA](#org1f1a57e)
  - [Set up controller repo](#org72c4bcf)
  - [Use the codeql extension to run MRVA](#org5edd48e)
  - [Use custom list with target repos in VS Code](#org93ceb2d)
  - [Run MRVA from command line](#org18c5e86)
- [Miscellaneous Notes](#org1d0d4b5)
  - [Action logs on Controller Repository](#orge8b438e)


<a id="org1f1a57e"></a>

# Using MRVA

Following are notes to illustrate a full MRVA workflow.


<a id="org72c4bcf"></a>

## Set up controller repo

Following [the instructions](https://codeql.github.com/docs/codeql-for-visual-studio-code/running-codeql-queries-at-scale-with-mrva/#controller-repository), start with manually creating the controller repository

```sh
gh repo create mirva-controller --public -d 'Controller for MRVA'
```

This avoids

```text
An error occurred while setting up the controller repository: Controller
repository "hohn/mirva-controller" not found.
```

Populate the controller repository

```sh
mkdir -p ~/local/mirva-controller && cd ~/local/mirva-controller 
echo "* mirva-controller" >> README.org
git init
git add README.org
git commit -m "first commit"
git branch -M master
git remote add origin git@github.com:hohn/mirva-controller.git
git push -u origin master
```

This avoids

```text
Variant analysis failed because the controller repository hohn/mirva-controller
does not have a branch 'master'. Please create a 'master' branch by clicking here
and re-run the variant analysis query. 
```


<a id="org5edd48e"></a>

## Use the codeql extension to run MRVA

Following the [instructions](https://codeql.github.com/docs/codeql-for-visual-studio-code/running-codeql-queries-at-scale-with-mrva/#controller-repository) and running `./FlatBuffersFunc.ql`, the entry `google/flatbuffers` has one [result](https://github.com/google/flatbuffers/blob/dbce69c63b0f3cee8f6d9521479fd3b087338314/src/binary_annotator.cpp#L25C21-L25C37). Others have none.


<a id="org93ceb2d"></a>

## Use custom list with target repos in VS Code

The json file is in your VS Code workspace. In my case, here:

    /Users/hohn/Library/Application Support/Code/User/workspaceStorage/bced2e4aa1a5f78ca07cf9e09151b1af/GitHub.vscode-codeql/databases.json

It can be edited in VS Code using the `{}` button.

It's saved in the workspace, but not in the current git repository.

Here are two snapshots for reference:

```javascript
{
    "version": 1,
    "databases": {
        "variantAnalysis": {
            "repositoryLists": [
                {
                    "name": "mirva-list",
                    "repositories": [
                        "google/flatbuffers"
                    ]
                }
            ],
            "owners": [],
            "repositories": []
        }
    },
    "selected": {
        "kind": "variantAnalysisSystemDefinedList",
        "listName": "top_10"
    }
}
```

or

```javascript
{
    "version": 1,
    "databases": {
        "variantAnalysis": {
            "repositoryLists": [
                {
                    "name": "mirva-list",
                    "repositories": [
                        "google/flatbuffers"
                    ]
                }
            ],
            "owners": [],
            "repositories": []
        }
    },
    "selected": {
        "kind": "variantAnalysisUserDefinedList",
        "listName": "mirva-list"
    }
}
```


<a id="org18c5e86"></a>

## Run MRVA from command line

1.  Install mrva cli
    
    ```sh
    cd ~/local/gh-mrva
    # Build it
    go mod edit -replace="github.com/GitHubSecurityLab/gh-mrva=/Users/hohn/local/gh-mrva"
    go build
    
    # Install 
    gh extension install .
    
    # Sanity check
    gh mrva -h
    ```

2.  Set up the configuration
    
    ```sh
    cd ~/local/gh-mrva
    
    cat > ~/.config/gh-mrva/config.yml <<eof
    # The following options are supported
    # codeql_path: Path to CodeQL distribution (checkout of codeql repo)
    # controller: NWO of the MRVA controller to use
    # list_file: Path to the JSON file containing the target repos
    
    # git checkout codeql-cli/v2.15.5
    codeql_path: /Users/hohn/local/codeql-lib
    controller: hohn/mirva-controller
    list_file: /Users/hohn/local/gh-mrva/databases.json
    
    eof
    ```

3.  Submit the mrva job
    
    ```sh
    gh mrva submit --help    
    
    gh mrva submit --language cpp --session mirva-session-1 \
       --list mirva-list                                    \
       --query /Users/hohn/local/gh-mrva/FlatBuffersFunc.ql
    ```

4.  Check the status and download the sarif files
    
    ```sh
    cd ~/local/gh-mrva
    
    # Check the status
    gh mrva status --session mirva-session-1
    
    # Download the sarif files when finished
    gh mrva download --session mirva-session-1 \
       --output-dir mirva-session-1-sarif
    
    # Or download the sarif files and CodeQL dbs when finished
    gh mrva download --session mirva-session-1 \
       --download-dbs \
       --output-dir mirva-session-1-sarif
    ```


<a id="org1d0d4b5"></a>

# Miscellaneous Notes


<a id="orge8b438e"></a>

## Action logs on Controller Repository

The action logs are on the controller repository at <https://github.com/hohn/mirva-controller/actions>.

The `action>google flatbuffers` log references

    github/codeql-variant-analysis-action

```yaml
Run actions/checkout@v4
with:
    repository: github/codeql-variant-analysis-action
    ref: main
    token: ***
    ssh-strict: true
    persist-credentials: true
    clean: true
    sparse-checkout-cone-mode: true
    fetch-depth: 1
    fetch-tags: false
    show-progress: true
    lfs: false
    submodules: false
    set-safe-directory: true
    env:
        CODEQL_ENABLE_EXPERIMENTAL_FEATURES_SWIFT: true
```

This is <https://github.com/github/codeql-variant-analysis-action>

The workflow producing the logs: <https://github.com/github/codeql-variant-analysis-action/blob/main/variant-analysis-workflow.yml>