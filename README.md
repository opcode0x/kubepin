# KubePin

`kubepin` runs several Kubernetes service port forwards from one command. Define mappings in JSON, YAML, or dotenv values. Each mapping binds to `127.0.0.1` and stays active until you press Ctrl+C. If a forward exits, `kubepin` restarts it automatically with a retry delay that grows from one to 30 seconds during repeated failures.

## Requirements

- Go 1.22 or newer to build
- `kubectl` on your `PATH`, with access to the selected Kubernetes context

## Build

```sh
go build -o kubepin .
```

## Homebrew

After the first tagged release, install the CLI from the [KubePin Homebrew tap](https://github.com/opcode0x/homebrew-tap):

```sh
brew install opcode0x/tap/kubepin
```

The formula builds the CLI from source and installs `kubectl` as a dependency. The planned macOS menu bar app will be a separate Homebrew cask, so installing the CLI will not install a GUI.

## YAML configuration

Copy `kubepin.example.yaml` to `kubepin.yaml` and `.env.example` to `.env`, then set your context and ports:

```yaml
context: development
namespace: default
env_file: .env
forwards:
  - service: postgres
    namespace: data
    ports:
      - "${POSTGRES_LOCAL_PORT}:5432"
  - service: redis
    ports:
      - "16379:6379"
```

```sh
./kubepin -dry-run
./kubepin
```

`kubepin` discovers `kubepin.yaml`, `kubepin.yml`, then `kubepin.json` in the current directory. Use `-config path` to choose another file. The `env_file` path is relative to the config file. A `-env path` override is relative to the current directory. If neither is set and `.env` exists in the current directory, it is loaded automatically. Process environment values override dotenv values; `-context` and `-namespace` override the context and default namespace. Each service can specify its own `namespace`; when omitted, the top-level namespace applies.

## JSON configuration

```json
{
  "context": "development",
  "namespace": "default",
  "env_file": ".env",
  "forwards": [
    {"service": "postgres", "namespace": "data", "ports": ["${POSTGRES_LOCAL_PORT}:5432"]},
    {"service": "redis", "ports": ["16379:6379"]}
  ]
}
```

Use `./kubepin -config kubepin.json` when both formats are present.

## Environment-only configuration

No config file is required. Put this in `.env`, or export the values in your shell:

```dotenv
KUBEPIN_CONTEXT=development
KUBEPIN_NAMESPACE=default
KUBEPIN_FORWARD_POSTGRES=15432:5432
KUBEPIN_NAMESPACE_POSTGRES=data
KUBEPIN_FORWARD_REDIS=16379:6379
```

Each `KUBEPIN_FORWARD_<SERVICE>` value can contain comma-separated `LOCAL:REMOTE` mappings. `KUBEPIN_NAMESPACE_<SERVICE>` sets the namespace for that service, including one defined in a config file. Underscores in the variable suffix become hyphens in the Kubernetes service name. Environment mappings are added to mappings from a config file. Duplicate local ports, missing variables, malformed mappings, and unknown config fields fail before any forwards start.

Run `./kubepin -h` for flags. Run `go test ./...` for tests.

## Contributing

Issues and pull requests are welcome. Please include a short explanation of the change and run `go test ./...`. This project is licensed under MIT; see [LICENSE](LICENSE).
