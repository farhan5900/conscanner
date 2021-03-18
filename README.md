# CONScanner (Container Scanner)

A Tool that can scan the given directory for mentioned container images and run the CVE Reporter for those images.

## Prerequisite
- `curl`
- [`grype`](https://github.com/anchore/grype#installation)

## Build

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o conscanner
```

## Run

To scan the directory for container images:

```bash
./conscanner images /path/to/directory
```

The command will read all the `YAML` files in the directory and create `images.json` file as an output.

*NOTE:* Instead of providing directory, we can also provide a `YAML` file directly.

To get the `CVE` reports:

```bash
./conscanner report /path/to/images.json
```

This command will generate reports for each image and store it in a directory named `conscanner`.
