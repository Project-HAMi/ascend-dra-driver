# Repository Guidelines

## Project Structure & Module Organization

Executables live in `cmd/ascend-dra-kubeletplugin/` and `cmd/ascend-dra-tester/`. Shared Go code is under `pkg/`; versioned API types and validation are in `api/project-hami.io/resource/npu/v1alpha1/`. Helm resources are in `deployments/helm/ascend-dra-driver/`, and the container definition is in `deployments/container/`. Use `demo/` for cluster examples and `dev/` for debugging helpers. The `hami-vnpu-core` and `third_party` trees are Git submodules.

## Build, Test, and Development Commands

- `make submodules`: initialize and update required submodules.
- `make build`: compile all Go packages for Linux.
- `make binaries`: build both commands with version metadata and debug-friendly flags.
- `make test`: build commands, run unit tests, and write `coverage.out`.
- `make coverage`: print function-level coverage excluding generated mocks.
- `make check`: run formatting, vet, lint, ineffassign, and spelling checks.
- `make verify-helm-chart`: lint and render the Helm chart, then verify key settings.
- `make libvnpu-artifacts && make image`: build the sharing library and container image; requires Ascend development libraries.

Go 1.26.2, GNU Make, and initialized submodules are required.

## Coding Style & Naming Conventions

Format Go with `gofmt -s` (`make fmt`) and keep imports compatible with `goimports`. Exported identifiers use `PascalCase`, internal identifiers use `camelCase`, and package names are short lowercase words. Name files by responsibility, using underscores only for generated or test files. Run `make assert-fmt`, `make vet`, and `make lint` before submitting. Do not hand-edit `zz_generated.deepcopy.go`; regenerate it with `make generate`.

## Testing Guidelines

Tests use Go's `testing` package with `testify` where useful. Keep tests beside implementation in `*_test.go` files and name cases `TestXxx`; table-driven tests are preferred for validation and allocation behavior. Run `go test ./...` for quick iteration and `make test` for the CI-equivalent suite. Changes to allocation lifecycle code should cover discovery, resource publication, Prepare/Unprepare, CDI edits, checkpointing, and error cleanup. Demo contract tests are in `demo/kind-vnpu/scripts/test_demo_contract.py`.

## Agent-Specific Instructions

Before work, check for `AGENTS.local.md` at repository root. If present, read and follow it; its instructions take precedence over this guide.

## Commit & Pull Request Guidelines

Recent history follows concise Conventional Commit subjects such as `feat: improve demo` and `fix: get action version ...`. Use an imperative `feat:`, `fix:`, `chore:`, or similar prefix, and keep each commit focused. Pull requests should explain motivation and behavior, link relevant issues, list verification commands, and call out Helm, image, hardware, or feature-gate impacts. Include logs or rendered configuration when they clarify operational changes. All contributions require DCO sign-off (`git commit -s`).
