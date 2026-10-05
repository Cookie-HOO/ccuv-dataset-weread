# CCUV WeRead Dataset

Native `ccuv.custom/v1` dataset adapter for WeRead reading time. The stable dataset ID is `weread`; after installation, ccuv invokes:

```text
ccuv-dataset-weread ccuv
```

## Current status

This repository provides the protocol, bounded production gateway, deterministic package, and strict archive foundation. The gateway requests annual detail first and falls back to verified monthly detail when the annual response does not provide daily buckets. It is still **unreleased**: a fresh Dashboard render against the authorized source must succeed before any official catalog entry is published.

`request_kind: "probe"` returns the stable descriptor without constructing the HTTP gateway, reading business data, requiring `WEREAD_API_KEY`, or making a network request. Ranking declares `period: "1mo"`, `group_by: "book"`, and `top: 10`; its grouped chart request rules remain authoritative.

The intended first artifact targets `darwin/arm64` and contains exactly:

```text
manifest.json
bin/ccuv-dataset-weread
```

No README, interpreter, source tree, credential, or additional runtime file is accepted by ccuv's installer.

## Data semantics

- Metric: additive daily reading time in minutes, with two decimal places.
- Views: Timeline, Calendar, Stack, and Ranking support daily ungrouped Total reading time for arbitrary source-backed date ranges.
- The adapter retains exact integer source seconds internally, then converts outward metric points to minutes rounded to two decimal places. Summary totals sum raw seconds before one final conversion and rounding.
- WeRead source dates and source requests always use `Asia/Shanghai`. A valid request timezone other than `Asia/Shanghai` succeeds with a localized `timezone_not_supported` warning; it does not change source calendar interpretation.
- The adapter uses annual `dailyReadTimes` when available and otherwise requests a month-specific `readTimes` map. An omitted date is treated as zero only when that response's authoritative `totalReadTime` exactly equals the sum of its returned daily map; otherwise it returns `range_not_available`. Current dates are decided by this true source-coverage rule, not a local clock cutoff.
- Ranking declares the cycle `Total → Book → Category`. Grouped selections are Dataset-owned: they require the semantic `range.period` `1w`, `1mo`, or `1y` (week-to-date, month-to-date, or year-to-date). Other group/period combinations return `unsupported_query` with `unsupported: {"field": "range.period", "value": "<selected period>"}`. ccuv clears a previously accepted graph for this field, then the localized response names the supported periods; it never silently changes the selection or substitutes Total.

### Natural-period grouped summaries

WeRead does not expose complete per-entity daily attribution. Grouped results are therefore **Ranking-only** official natural-period summaries: each returned entity has one point at the requested `end_date`; that coordinate is not a claim of daily attribution. Timeline, Stack, and Calendar remain Total-only for these groupings.

- **Book** uses the source's `readLongest` official top-content collection. It is source-capped (currently up to ten entries), so it is not a complete book history or a reconciliation with Total.
- **Category** uses `preferCategory`, an official preference/duration summary (currently source-capped, typically up to eight entries), not an exhaustive category ranking.
- **Author** is not advertised because the source's thresholded `preferAuthor` preference summary exposes formatted duration text rather than verified numeric seconds. Any grouping outside the declared Book and Category allowlist returns `unsupported_query` with `unsupported: {"field": "query.group_by", "value": "<selected grouping>"}` before the gateway is contacted. The localized message names the supported groupings without prescribing a host control. ccuv preserves an already accepted chart for this non-period rejection; the adapter never estimates or parses a display string as duration.

Requests always include inclusive absolute dates; `range.period` carries the selected rolling-period semantic and must agree with those dates. A fixed range sends `period: null` and cannot request a grouped summary. The protocol identifier remains `ccuv.custom/v1`.

The adapter does not offer account or free-form filtering. Tests use synthetic or redacted fixtures only—never account history or credentials.

## Credentials and privacy

The release manifest declares `WEREAD_API_KEY` as required and sensitive. A future verified gateway implementation will read it only through `os.LookupEnv` and send it only in an HTTPS Bearer authorization header. It must never accept it as a command argument, write it to disk, include it in diagnostics, or emit raw source responses.

## Development

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
./scripts/package.sh
go run ./cmd/archiveverify dist/ccuv-dataset-weread-darwin-arm64.tar.gz
```

Tests use only fake gateway records and do not contact a live account.

## Release gate

Tags currently validate and retain an unpublished archive only; the workflow cannot create a GitHub Release. Do not enable official publication until a real gateway response contract has been validated without recording personal data, the network adapter has bounded/redacted failure handling, and the generated archive passes both this repository's verifier and ccuv's archive validator.
