# gh-mrva

## Configuration

A configuration file will be created in `~/.config/mrva/config.yml`. The following options are supported:
- `controller`: NWO of the MRVA controller to use
- `listFile`: Path to the JSON file containing the target repos

## Usage

Until the extension gets published you can use `go run .` instead of `gh mrva`

### Submit a new query

```bash
gh mrva submit [--controller <CONTROLLER>] --lang <LANGUAGE> [--list-file <LISTFILE>] --list <LIST> --query <QUERY> [--name <NAME>]
```

Note: `controller` and `list-file` are only optionals if defined in the configuration file
Note: if a `name` (any arbitrary name) is provided, the resulting run IDs will be stored in the configuration file so they can be referenced later for download

### Download the results

```bash
gh mrva download [--controller <CONTROLLER>] --lang <LANGUAGE> --output-dir <OUTPUTDIR> [--name <NAME> | --run <ID>] [--download-dbs]
```

Note: `controller` is only optionals if defined in the configuration file
Note: if a `name` is provided, the run ID is not necessary and instead `gh-mrva` will download the artifacts associated that `name` as found in the configuration file

## Contributing

gh-mrva is a work in progress. If you have ideas for new fixes or improvements, please open an issue or pull request.

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
