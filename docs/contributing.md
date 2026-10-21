# Contributing

Thanks for helping with muscles. Issues and pull requests are welcome: bugs, docs, new notes (detectors), and features.

The full guide is [CONTRIBUTING.md](https://github.com/rossedman/muscles/blob/main/CONTRIBUTING.md) in the repository. In short:

```bash
git clone https://github.com/rossedman/muscles && cd muscles
make check      # gofmt, go vet, one command per file, doc comments, go test
make cover      # total coverage must stay at 90% or more
make lint       # golangci-lint
make vuln       # govulncheck
make docs       # build this site (needs: pip install -r docs/requirements.txt)
```

- Open an issue first for anything bigger than a small fix.
- Read [Design](design.md) before changing how something works.
- A behavior change isn't done until its docs are: the page here, [CLI reference](cli.md), and the README.
- Every function has a doc comment, and the [critical paths](https://github.com/rossedman/muscles/blob/main/CONTRIBUTING.md#critical-paths) have benchmarks and fuzz targets.
- Every command lives in its own file in `internal/cli/` with `Short`, `Long` and three `Example` lines.

For a security problem, don't open an issue: see [SECURITY.md](https://github.com/rossedman/muscles/blob/main/SECURITY.md).
