# WeRead V2 Metric Package Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the Dataset to V2-only typed routing, one package per Metric, and generated manifests without changing the public V2 contract.

**Architecture:** `internal/protocol/v2` handles wire JSON; `internal/dataset` combines descriptors, routes Metrics, and generates manifests; each Metric package owns its semantic logic; `internal/weread` remains the shared source adapter.

**Tech Stack:** Go 1.27 standard library, `go test`, `go vet`, POSIX shell.

**Spec:** `docs/superpowers/specs/2026-10-09-v2-metric-package-refactor-design.md`

## Constraints

- Parse only `ccuv.custom/v2`; V1 requests are rejected.
- Preserve V2 field names, values, errors, localized copy, IDs, warnings, and `ccuv-dataset-weread ccuv`.
- Preserve IDs `weread`, `reading_time`, `total_notes`; timezone `Asia/Shanghai`; version `0.0.1`; revision `weread-2`.
- Probe must not create the gateway, read `WEREAD_API_KEY`, or make HTTP requests.
- Metrics live at `internal/metrics/readingtime` and `internal/metrics/totalnotes`, with no cross-Metric imports.
- `manifest.json` is generated Darwin/arm64 output from the Probe descriptor source.
- Do not stage the untracked local binary.

## Task 1: Create The Typed V2 Protocol

**Files:**
- Create: `internal/protocol/v2/protocol.go`
- Create: `internal/protocol/v2/protocol_test.go`
- Delete: `internal/protocol/protocol.go`
- Delete: `internal/protocol/protocol_test.go`

**Produces:** `Version`, typed `Request`, `ChartRequest`, `SummaryRequest`, `Response`, `Decode`, `InvalidRequestFor`, `ErrorFor`, and `Encode`.

- [ ] Write failing tests for a V2 chart with unknown fields, rejected V1, trailing JSON, and recoverable invalid V2 envelopes.
- [ ] Run `go test ./internal/protocol/v2 -count=1`; expect package-not-found failure.
- [ ] Move the existing JSON-tagged localized text, error, descriptor, warning, series, point, unit, capability, and summary models to `internal/protocol/v2`.
- [ ] Implement strict one-document parsing. Validate V2 Probe/Chart/Summary shapes and trustworthy envelope IDs; retain unknown request fields to preserve existing forward compatibility.
- [ ] Make V1 invalid at decode time. `InvalidRequestFor` returns a correlated `invalid_request` only for a trusted V2 envelope.
- [ ] Run `gofmt -w internal/protocol/v2 && go test ./internal/protocol/v2 -count=1`; expect PASS.
- [ ] Commit: `git add internal/protocol/v2 internal/protocol && git commit -m "refactor: add typed v2 dataset protocol"`.

## Task 2: Split Metrics Into Independent Packages

**Files:**
- Create: `internal/metrics/readingtime/metric.go`
- Create: `internal/metrics/readingtime/metric_test.go`
- Create: `internal/metrics/totalnotes/metric.go`
- Create: `internal/metrics/totalnotes/metric_test.go`
- Modify: `internal/weread/weread.go`
- Modify: `internal/weread/weread_test.go`

**Produces:** `readingtime.ID`, `readingtime.Descriptor`, `readingtime.Handler`; `totalnotes.ID`, `totalnotes.Descriptor`, `totalnotes.Handler`; typed source records:

```go
type DailyRecord struct { Date string; Seconds uint64 }
type ReadingRanking struct { ID string; Label string; Seconds uint64 }
type NoteCount struct { BookID string; Label string; Count uint64 }
```

- [ ] Write failing reading-time tests for `28` seconds becoming wire value `"0.47"`, raw seconds summed before summary rounding, missing daily coverage, and Book/Category natural-period ranking validation.
- [ ] Write failing notes tests for all-time Book Ranking, total summary, rejected non-`all` periods, rejected non-Book grouping, and rejected non-total summary scope before a source call.
- [ ] Run `go test ./internal/metrics/readingtime ./internal/metrics/totalnotes -count=1`; expect package-not-found failure.
- [ ] Implement `readingtime.Source` with `DailyRecords(context.Context, []v2.Range)` and `PeriodRanking(context.Context, v2.Range, string)`. Move descriptor, current query rules, raw-second coverage/aggregation, formatting, warnings, and localized errors into this package.
- [ ] Implement `totalnotes.Source` with `NotebookCounts(context.Context)`. Move descriptor, all-time Book ranking validation, total-summary validation, source error handling, and response construction into this package.
- [ ] Change shared source/FakeGateway fixtures to typed numeric records. The adapter validates source numbers once; the Metric packages alone create protocol decimal strings.
- [ ] Run `gofmt -w internal/metrics internal/weread && go test ./internal/metrics/readingtime ./internal/metrics/totalnotes ./internal/weread -count=1`; expect PASS.
- [ ] Commit: `git add internal/metrics internal/weread && git commit -m "refactor: isolate weread metric handlers"`.

## Task 3: Add Dataset Registry And Generated Manifest

**Files:**
- Create: `internal/dataset/dataset.go`
- Create: `internal/dataset/dataset_test.go`
- Create: `cmd/manifest/main.go`
- Create: `cmd/manifest/main_test.go`
- Modify: `manifest.json`

**Produces:** `dataset.Descriptor()`, `dataset.NewRegistry(factory)`, `Registry.Handle`, `dataset.Manifest(os, arch)`, and `dataset.WriteManifest`.

- [ ] Write failing tests proving a Darwin manifest descriptor is `reflect.DeepEqual` to `dataset.Descriptor()`, and a Windows manifest entrypoint is `bin/ccuv-dataset-weread.exe`.
- [ ] Run `go test ./internal/dataset ./cmd/manifest -count=1`; expect package-not-found failure.
- [ ] Implement Dataset metadata and environment declaration. Assemble descriptors in stable order: `reading_time`, then `total_notes`.
- [ ] Implement `Registry.Handle(ctx, request)`: Probe returns immediately; Chart/Summary dispatch by `metric_id`; unrecognized Metric/request kind returns the current correlated `unsupported_chart_kind` error.
- [ ] Implement `cmd/manifest --os OS --arch ARCH --out PATH`. It writes newline-terminated deterministic JSON; positional arguments exit 2.
- [ ] Run `go run ./cmd/manifest --os darwin --arch arm64 --out manifest.json`.
- [ ] Run `gofmt -w internal/dataset cmd/manifest && go test ./internal/dataset ./cmd/manifest -count=1`; expect PASS.
- [ ] Commit: `git add internal/dataset cmd/manifest manifest.json && git commit -m "refactor: generate weread manifest from descriptor"`.

## Task 4: Turn The Command Into A Thin V2 Boundary

**Files:**
- Modify: `cmd/ccuv-dataset-weread/main.go`
- Modify: `cmd/ccuv-dataset-weread/main_test.go`
- Delete: `testdata/chart-request.json`

**Produces:** `handle(input []byte, factory func() (dataset.Gateway, error)) (v2.Response, error)`.

- [ ] Replace V1 tests with V2 JSON fixtures covering no-gateway Probe, reading-time chart, total-notes chart/summary, invalid query, missing credential, and rejected V1.
- [ ] Run `go test ./cmd/ccuv-dataset-weread -count=1`; expect failure while legacy paths remain.
- [ ] Replace command internals with: decode V2, return recoverable V2 invalid-request when possible, then call `dataset.NewRegistry(factory).Handle(context.Background(), request)`.
- [ ] Delete V1 types, V2-to-V1 conversion, response maps, static descriptor maps, and all Metric-specific helper functions from the command.
- [ ] Retain argument handling and stderr-only diagnostics.
- [ ] Run `gofmt -w cmd/ccuv-dataset-weread && go test ./cmd/ccuv-dataset-weread ./internal/... -count=1`; expect PASS.
- [ ] Commit: `git add cmd/ccuv-dataset-weread testdata internal && git commit -m "refactor: route weread command through v2 registry"`.

## Task 5: Bound Notebook Retrieval And Update Release Automation

**Files:**
- Modify: `internal/weread/gateway.go`
- Modify: `internal/weread/gateway_test.go`
- Modify: `scripts/package.sh`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `README.md`

- [ ] Add failing gateway tests for duplicate Book IDs across pages yielding one count and an endless response exceeding `maxNotebookPages` yielding a categorized redacted source failure.
- [ ] Run `go test ./internal/weread -run 'TestNotebookCountsDeduplicatesBooksAcrossPages|TestNotebookCountsRejectsPageBudgetExhaustion' -count=1`; expect failure.
- [ ] Implement `NotebookCounts` with `maxNotebookPages = 100` and `maxNotebookRecords = 10000`. Reject repeated cursors, exhausted budgets, duplicate IDs, malformed records, and overflow. Preserve `/user/notebooks` payload and `reviewCount + noteCount + bookmarkCount` semantics.
- [ ] Replace the `sed` block in `scripts/package.sh` with `go run ./cmd/manifest --os "$target_os" --arch "$target_arch" --out "$work/manifest.json"`.
- [ ] Fix CI/release package invocations to `./scripts/package.sh darwin arm64 dist/first.tar.gz`. Generate `dist/generated-manifest.json` and compare it to root `manifest.json` before package verification.
- [ ] Replace README V1 wording with V2-only wording, document `cmd/manifest`, and remove the obsolete “future gateway” claim.
- [ ] Run these checks:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go run ./cmd/manifest --os darwin --arch arm64 --out /tmp/weread-manifest.json
cmp manifest.json /tmp/weread-manifest.json
./scripts/package.sh darwin arm64 dist/refactor-first.tar.gz
./scripts/package.sh darwin arm64 dist/refactor-second.tar.gz
cmp dist/refactor-first.tar.gz dist/refactor-second.tar.gz
go run ./cmd/archiveverify dist/refactor-first.tar.gz
```

- [ ] Expect every command to exit 0 and both `cmp` commands to print nothing.
- [ ] Commit: `git add internal/weread scripts/package.sh .github/workflows README.md manifest.json && git commit -m "build: generate weread manifests for releases"`.

## Task 6: Final V2 Compatibility Audit

**Files:**
- Modify: `cmd/ccuv-dataset-weread/main_test.go`
- Modify: `internal/dataset/dataset_test.go`
- Modify: `README.md`

- [ ] Add a regression proving a handled Probe descriptor is `reflect.DeepEqual` to `dataset.Manifest("darwin", "arm64").Probe.Descriptor`.
- [ ] Add chart and summary regressions proving numeric values remain JSON strings, never JSON numbers.
- [ ] Run `go test ./cmd/ccuv-dataset-weread ./internal/dataset -count=1`; expect PASS.
- [ ] Run `rg -n 'custom/v1|V1|v1' README.md manifest.json cmd internal --glob '*.go' --glob '*.md' --glob '*.json'`; expect no user-facing V1 support claim. A rejection-test identifier may retain `V1`.
- [ ] Run `git diff --check HEAD~5..HEAD && git status --short && go test ./... && go vet ./...`; expect clean whitespace and passing checks while the untracked local binary stays unstaged.
- [ ] Commit: `git add cmd/ccuv-dataset-weread/main_test.go internal/dataset/dataset_test.go README.md && git commit -m "test: lock weread v2 protocol compatibility"`.
