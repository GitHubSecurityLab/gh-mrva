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
