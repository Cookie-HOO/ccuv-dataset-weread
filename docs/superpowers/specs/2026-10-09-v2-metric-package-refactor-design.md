# WeRead V2 Metric Package Refactor Design

## Goal

Refactor the WeRead Dataset around the already-declared `ccuv.custom/v2`
protocol. The executable must preserve its existing V2 stdin/stdout JSON
contract, command invocation, metric IDs, descriptors, error codes, and
manifest semantics while removing the obsolete V1 runtime implementation.

Every Metric must have its own package and path so that its descriptor, query
validation, source projection, and response construction can evolve without
making the command entrypoint a business-logic coordinator.

## Scope And Compatibility

The Dataset accepts only `ccuv.custom/v2` after this refactor. V1 runtime
requests are intentionally removed because the manifest and Probe already
advertise V2.

The following V2 external behavior remains unchanged:

- executable invocation: `ccuv-dataset-weread ccuv`;
- one JSON request on stdin and one JSON response on stdout;
- Dataset ID `weread` and Metric IDs `reading_time` and `total_notes`;
- request and response field names, localized messages, warnings, status and
  error-code meanings;
- permitted Metric/chart/range/grouping combinations;
- package archive shape and generated `manifest.json` semantics.

The implementation may replace internal representation types and package
structure, but it must not add a user-visible protocol version, change a value
format, or silently accept an unsupported query.

## Architecture

```text
cmd/ccuv-dataset-weread
  stdin -> protocol/v2 decode -> dataset.Registry dispatch -> protocol/v2 encode -> stdout

internal/dataset
  Dataset metadata + environment declarations + Metric registry + manifest generation

internal/metrics/readingtime
  reading_time descriptor + V2 chart/summary validation + response projection

internal/metrics/totalnotes
  total_notes descriptor + V2 chart/summary validation + response projection

internal/weread
  shared authenticated WeRead HTTP gateway and source-specific data records
```

`cmd/ccuv-dataset-weread` owns process concerns only: argument validation,
bounded request input, gateway construction after a data request is validated,
and protocol output. It contains no Metric-specific selections, descriptors, or
response maps.

`internal/protocol/v2` is the only wire model. It strictly decodes one JSON
document, validates the common V2 envelope, exposes typed requests for Probe,
Chart, and Summary, and constructs correlated typed success/error responses.
It may retain forward-compatible unknown request fields where the current V2
contract already permits them, but all fields that determine semantics are
validated before dispatch.

`internal/dataset.Registry` answers Probe using the same Dataset descriptor that
manifest generation consumes. It maps a Chart or Summary `metric_id` to the
corresponding Metric package. It owns Dataset-wide availability errors and
exposes a single handler interface to the command.

Each Metric package owns a small handler and descriptor. It depends on an
interface exposing only the source operation it needs. `readingtime` depends on
daily records and natural-period rankings. `totalnotes` depends on all-time
notebook counts. Shared HTTP transport stays in `internal/weread`; no Metric
package imports another Metric package.

## Data Flow

1. The command decodes a V2 request.
2. Probe returns the Dataset descriptor without creating an HTTP gateway.
3. Chart and Summary requests are routed by `metric_id` to exactly one Metric
   handler.
4. The selected Metric validates its source-owned query constraints before the
   gateway is constructed or called.
5. The Metric asks its narrow source interface for typed source facts, applies
   its own coverage/aggregation rules, and returns a typed V2 response.
6. Protocol encoding writes exactly one JSON response to stdout.

Source values stay typed internally. Reading durations use raw integer seconds
until the reading-time Metric performs the existing minute conversion and
rounding. Note counts use unsigned integer counts. Decimal strings are created
only at the V2 boundary.

## Descriptor And Manifest Generation

The Dataset descriptor is code-owned by `internal/dataset`. It combines static
Dataset metadata, environment declarations, and descriptors returned by the
Metric packages in stable Metric-ID order.

A new `cmd/manifest` command writes deterministic JSON for a target OS and
architecture. `scripts/package.sh` invokes it instead of editing a hand-written
`manifest.json` with `sed`. The root `manifest.json` is the generated default
Darwin/arm64 manifest and remains committed for inspection and release review.

Tests must assert that:

- the runtime Probe descriptor equals the descriptor embedded in a generated
  manifest for the same platform;
- regenerating the root manifest produces no diff;
- generated Windows manifests use the `.exe` entrypoint while preserving all
  descriptor content.

## Errors And Privacy

Common malformed V2 input produces a correlated `invalid_request` response only
when the request envelope provides a valid correlation identity. Otherwise the
process fails without fabricating a response, matching the existing safety
principle.

Metric handlers preserve existing `unsupported_query`, `range_not_available`,
and `source_unavailable` behavior. The Dataset never substitutes a different
period, grouping, or zero value for unavailable source coverage.

The gateway remains the only layer that reads `WEREAD_API_KEY`. It must not
persist the credential or include it in returned errors, logs, manifested data,
or test fixtures. Notebook pagination adopts explicit page/record limits and
deduplicates book IDs before producing note-count facts.

## Verification

- Unit tests cover V2 request parsing and each Metric handler in its own
  package.
- Command tests exercise Probe, routing, missing credentials, and both Metrics
  through their stable V2 JSON fixtures.
- Manifest tests compare generated manifests and Probe descriptors.
- Gateway tests retain daily coverage, source timezone, pagination, bounded
  response, and credential-redaction coverage.
- `gofmt -w cmd internal`, `go test ./...`, and `go vet ./...` pass.
- The release packager generates archives for every supported platform and
  archive verification still accepts each result.
