# Repository Guidelines

## Project Structure & Module Organization

The Go module implements a Kubernetes Dynamic Resource Allocation driver for Ascend NPUs. Entry points live in `cmd/ascend-dra-kubeletplugin` and `cmd/ascend-dra-tester`; reusable packages are under `pkg/`. API types, validation, and generated deep-copy code are in `api/project-hami.io/resource/npu/v1alpha1`. Deployment assets live in `deployments/container` and `deployments/helm/ascend-dra-driver`. Use `demo/kind-vnpu` for the validated Kind workflow and `dev/` for host-side debugging helpers. `hami-vnpu-core` and `third_party/mind-cluster` are Git submodules; initialize them before building.

## Build, Test, and Development Commands

- `make submodules`: initialize and update nested dependencies.
- `make build`: compile all Go API, command, and package code.
- `make test`: build commands, run Go tests verbosely, and write `coverage.out`.
- `make check`: run formatting, vet, `golangci-lint`, ineffassign, and spelling checks.
- `make coverage`: report coverage after excluding generated mock files.
- `make verify-helm-chart`: lint and render the Helm chart with supported configurations.
- `make libvnpu-artifacts`: build the Rust `libvnpu.so`; this requires compatible Ascend development libraries.
- `make image`: build the container after libvnpu artifacts exist in `dist/hami-vnpu-core`.

Run a focused test with `go test ./cmd/ascend-dra-kubeletplugin -run TestName -v`.

## Coding Style & Naming Conventions

Format Go with `make fmt` (`gofmt -s`); Go indentation uses tabs. Follow standard Go naming: exported identifiers use `CamelCase`, internal identifiers use `camelCase`, and package names stay short and lowercase. Keep contextual logging compatible with `make logcheck`. Rust code in the submodule should pass `cargo fmt`. Do not hand-edit `zz_generated.deepcopy.go`; regenerate API artifacts with `make generate`.

## Testing Guidelines

Tests use Go's `testing` package, commonly with `testify`, and remain beside implementation files as `*_test.go`; name cases `TestXxx`. Add coverage for discovery, published resources, Prepare/Unprepare behavior, CDI edits, checkpoint persistence, and cleanup paths when changing allocation lifecycle code. Run `make test` before submission and Helm verification for chart changes. Hardware-dependent demo checks belong in `demo/kind-vnpu` and must document required Ascend hardware and runtime assumptions.

## Commit & Pull Request Guidelines

Recent history uses concise, imperative Conventional Commit-style subjects such as `feat: improve demo`; prefer `feat:`, `fix:`, `chore:`, or another accurate scope. Keep commits focused. Pull requests should explain behavior and risk, link relevant issues, list commands run, and include logs or rendered manifest snippets for deployment changes. Update documentation and tests together with behavior. All commits must satisfy the Project HAMi Developer Certificate of Origin (DCO), typically via `git commit -s`.
