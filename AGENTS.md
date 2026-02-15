# Repository Guidelines

## Project Structure & Module Organization
- `cmd/main.go`: entrypoint for manager/agent commands.
- `api/v1beta2/`: CRD API types and defaults for `RedisCluster`.
- `internal/controller/`: reconcile logic, finalizers, and controller tests.
- `internal/k8sutils/`: Kubernetes and Redis execution helpers (`cluster`, `statefulset`, `redisservice`, `k8smeta`).
- `config/`: CRDs, RBAC, manager manifests, and kustomize overlays.
- `charts/redis-operator/`: Helm chart packaging.
- `test/e2e/`: Ginkgo end-to-end suite (Kind-based).
- `docs/test/`, `minikube-test/`, `proxmox-test/`: manual scenario docs and local test scripts.

## Build, Test, and Development Commands
- `make build`: generate manifests/code, format/vet, build `bin/manager`.
- `make run` or `make run-manager`: run controller locally.
- `make run-agent`: run agent mode locally.
- `make test`: run non-e2e tests with envtest and coverage (`cover.out`).
- `make test-e2e`: create/use Kind cluster, run `./test/e2e`, then cleanup.
- `make lint` / `make lint-fix`: run golangci-lint checks (or autofix).
- `make docker-build IMG=<image:tag>`: build operator image.
- `make deploy IMG=<image:tag>` / `make undeploy`: deploy or remove controller via kustomize.

## Coding Style & Naming Conventions
- Use idiomatic Go and keep code `gofmt`-clean (`make fmt` is included in main targets).
- Package names should be short and lowercase; exported identifiers use `PascalCase`.
- Prefer small, focused functions in `internal/k8sutils/*` for command execution and parsing.
- Keep file naming consistent with purpose, e.g. `exec_nodes.go`, `*_test.go`.

## Testing Guidelines
- Frameworks: Go `testing` + Ginkgo/Gomega.
- Controller/unit tests live near source (`internal/**/_test.go`); envtest bootstrapping is in `internal/controller/suite_test.go`.
- E2E tests live in `test/e2e/` and assume Kind + optional cert-manager setup.
- Run `make test` before PRs; run `make test-e2e` for behavior changes in reconciliation, scaling, or failover.

## Commit & Pull Request Guidelines
- Prefer Conventional Commit style seen in history: `fix(scope): ...`, `docs(test): ...`, `chore(dev): ...`.
- Keep commits scoped to one change type (logic, tests, docs).
- PRs should include: intent, impacted paths, test evidence (`make test`, e2e/manual logs), and rollback notes for CRD/controller behavior changes.
- For operational changes, attach relevant outputs from `kubectl`, controller logs, and `docs/test/*.md` updates.
