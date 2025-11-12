# Repository Guidelines

## Project Structure & Module Organization
- `pkg/`, `cmd/`: Go controllers, plugins, and entrypoints.
- `config/`: Kustomize bases/overlays and CRDs; `dist/chart/`: Helm chart.
- `python/aibrix_kvcache/`: Python KV cache offloading library and tests.
- `samples/`: End‑to‑end examples (disaggregation, quickstart, etc.).
- `observability/`: Prometheus/Grafana monitoring configs.
- `test/`: Integration/e2e/regression; `.github/`: CI; `scripts/`, `hack/`, `docs/`.

## Build, Test, and Development Commands
- Build (Go): `make build` (manager binary), `make run` (run locally).
- Lint: `make lint` (golangci‑lint), `make lint-fix` (auto‑fix where possible).
- Tests (Go): `make test` (unit), `make test-integration` (Ginkgo), `make test-e2e` (Kind).
- Coverage: `make test-code-coverage` (writes `cover.out`).
- Containers: `make docker-build-all` / `make docker-push-all` (set `AIBRIX_CONTAINER_REGISTRY_NAMESPACE`).
- Dev on Kind: `make dev-install-in-kind`, `make dev-port-forward`, `make dev-uninstall-from-kind`.
- Python lib: `pip install -r python/aibrix_kvcache/requirements/{build,dev,core}.txt && pip install -e python/aibrix_kvcache`
  - Format/type/lint: `bash python/aibrix_kvcache/scripts/format.sh`
  - Tests: `pytest -q python/aibrix_kvcache/tests`

## Coding Style & Naming Conventions
- Go: `go fmt` enforced; lint via golangci‑lint (`.golangci.yml`). Package names lower‑case, no underscores. Files use `_test.go` for tests.
- Python: Ruff config in `pyproject.toml` (80 cols, import/order rules). Modules `snake_case`; 4‑space indent.
- Branches: `feature/<scope>`, `fix/<scope>`, `docs/<scope>` (e.g., `feature/sub-more-metrics`).

## Testing Guidelines
- Go unit/integration tests live under `./...` and `test/integration/...`; name tests with `_test.go` and table‑driven patterns where sensible.
- Python tests under `python/aibrix_kvcache/tests`; name `test_*.py`, prefer pytest fixtures.
- Before pushing: run `make lint test` and, for Python changes, the format script + pytest.

## Commit & Pull Request Guidelines
- Commit messages: concise imperative subject with a tag, e.g., `[Feat] Add PRIS connector cache policy`, `[Fix] Handle nil placement config (#1234)`.
- PRs: clear description, motivation, scope of change, linked issues, validation steps; include tests and docs/Helm chart updates as needed (screenshots for chart/UI changes).
- Keep PRs focused; rebase on `main` (sync with `upstream/main`) and resolve conflicts locally.

## Security & Configuration Tips
- Local dev requires Docker and a Kubernetes cluster (Kind recommended).
- Image namespace: set `AIBRIX_CONTAINER_REGISTRY_NAMESPACE=myrepo` before build/push.
- Python builds: set `AIBRIX_TARGET_DEVICE=cuda|cpu` as needed.
