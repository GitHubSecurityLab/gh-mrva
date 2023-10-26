# gh-mrva

> This is an unofficial tool and is not officially supported by GitHub.

## Configuration

A configuration file will be created in `~/.config/gh-mrva/config.yml`. The following options are supported:
- `codeql_path`: Path to CodeQL distribution (checkout of [codeql repo](https://github.com/github/codeql))
- `controller`: NWO of the MRVA controller to use
- `list_file`: Path to the JSON file containing the target repos

## Usage

### Submit a new query

```bash
gh mrva submit [--codeql-path<path to CodeQL repo>] [--controller <controller>] --language <language> --session <session name> [--list-file <list file>] --list <list> [--query <query> | --query-suite <query suite> ]
```

Note: `codeql-dist`, `controller` and `list-file` are only optionals if defined in the configuration file

### Download the results

```bash
gh mrva download --session <session name> --output-dir <output directory> [--download-dbs] [--nwo <owner/repo>]
```

### List sessions

```bash
gh mrva list [--json]
```

### Check scan status

```bash
gh mrva status --session <session name> [--json]
```

## Contributing

`gh-mrva` is a work in progress. If you have ideas for new fixes or improvements, please open an issue or pull request.

If possible, tests should be added for any new fixes. We favour testing with real file systems or processes where possible.

## Releasing

Releasing is currently done via tags. To release a new version, create a new tag and push it to the remote. The release workflow will automatically build and publish the new version.

e.g.

```sh
# Determine the latest tag
git tag -l | sort -V | tail -n 1

# Create a new tag
git tag -a v0.0.2 -m "Release v0.0.2"

# Push the tag to the remote
git push origin --tags
```
