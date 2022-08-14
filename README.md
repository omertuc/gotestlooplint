This linter looks for loop var capture in parallel go tests or for loop var
capture in regular Ginkgo tests. Both situations lead to tests executing in an
unexpected manner.

## Setup
To install the latest version, run:

```bash
go install github.com/omertuc/gotestlooplint/cmd/gotestlooplint@latest
```

To install a particular version, run:

```bash
go install github.com/omertuc/gotestlooplint/cmd/gotestlooplint@v0.1.0
```
