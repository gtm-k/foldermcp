# Benchmark Corpus Manifest Schema

Status: **schema for the FROZEN (Phase 9b) corpus.** The pilot manifest
(`pilot-corpus-manifest.yaml`) uses the offline variant of this schema described
in [§4](#4-pilot-variant-offline-copy).

This document is the contract that `build-corpus.sh` reads and that the future
`>=1000`-file frozen corpus will be authored against. It exists so that the
corpus is **declarative, reproducible, and content-addressed**: anyone with the
manifest and a network connection can rebuild a byte-identical corpus, and the
SHA-256 of every file is pinned so a silently-changed upstream artifact fails the
build instead of poisoning the benchmark.

---

## 1. Top-level structure

```yaml
version: 1                 # int, schema version (bump on breaking changes)
mode: frozen               # "frozen" (download+verify) | "pilot" (offline copy)
corpus_id: bench-v1        # str, stable id; becomes the git tag stem (bench-v1-frozen)
entries:                   # ordered list; build order == manifest order (deterministic)
  - { ...entry... }
```

`entries` is an **ordered** list. `build-corpus.sh` copies/downloads in manifest
order and lays files out at their `dest`, so the same manifest always yields the
same tree (D11 reproducibility requirement).

## 2. Entry fields (FROZEN)

| field    | type   | required | description |
|----------|--------|----------|-------------|
| `id`     | string | yes      | Stable, unique identifier for this file within the corpus. Used in logs and to diff manifests across revisions. Convention: `<stratum>-<short-slug>` (e.g. `code-go-worker-pool`). Never reused for different content. |
| `url`    | string | yes      | Absolute fetch URL. MUST be a pinned, immutable reference (a release asset, a tagged raw URL, or a commit-pinned `raw.githubusercontent.com/<owner>/<repo>/<sha>/...`). MUST NOT point at a mutable `HEAD`/`main` ref — that breaks content-addressing. |
| `sha256` | string | yes      | Lowercase hex SHA-256 of the fetched bytes. The build verifies the download against this and **fails closed** on mismatch. This is the freeze anchor: changing it (or the bytes it pins) voids the run under D11. |
| `license`| string | yes      | SPDX identifier or short license name of the source artifact (e.g. `MIT`, `Apache-2.0`, `CC-BY-4.0`, `public-domain`). Required so the assembled corpus is redistributable and auditable. Files whose license forbids redistribution MUST NOT be added. |
| `stratum`| enum   | yes      | Benchmark stratum the file belongs to. One of: `code`, `md`, `pdf`, `csv`. Drives per-stratum metric bucketing in the harness and the PDF-extraction kill-gate. (See [§3](#3-stratum-enum).) |
| `dest`   | string | yes      | Corpus-relative destination path (POSIX, forward slashes, no leading `/`, no `..`). This is where the file lands under the corpus root. Judgment paths in the labeled-query set are substring-matched against the indexed hit path, so `dest` values must be unique tails (don't make one a substring of another). |

### Optional entry fields

| field      | type   | description |
|------------|--------|-------------|
| `source`   | string | Human note on provenance (repo name, paper title, dataset). Documentation only. |
| `notes`    | string | Free text (e.g. "image-only PDF, no text layer — negative/edge fixture"). |

## 3. `stratum` enum

The stratum is the **benchmark unit of analysis**. It is derived from the file
**extension**, not from the indexer's `content_class` — because `content_class:
document` splits across two strata (Markdown prose vs. binary PDF/Office). The
canonical extension → stratum map:

| stratum | extensions | chunk_kinds produced |
|---------|-----------|----------------------|
| `code`  | `.go .py .js .ts .rs .java .c .cc .cpp .h .hpp .sh` | `code_ast`, `code_fallback` |
| `md`    | `.md .txt .rst .org .tex`                            | `prose` |
| `pdf`   | `.pdf .docx .odt`                                    | `pdf_text`, `office_text` |
| `csv`   | `.csv .tsv .json .yaml .yml .xml`                    | `csv_schema`, `csv_rows`, `data_structured` |

Files that classify as `image`, `media`, `unknown`, or binary containers
(e.g. `.sqlite`) are **not indexed** (produce no chunks) and therefore MUST NOT
appear in the manifest — they are outside every benchmark stratum.

## 4. Pilot variant (offline copy)

The pilot corpus is assembled **without network** by copying in-repo testdata
fixtures, so the pilot manifest replaces the two network/identity fields with a
single local-source field:

- **drop** `url` and `sha256`
- **add** `src` — repo-relative path of an existing, git-tracked fixture to copy.

Everything else (`id`, `license`, `stratum`, `dest`) is identical. A pilot entry:

```yaml
  - id: code-go-worker-pool
    stratum: code
    license: repo-internal-fixture
    src: testdata/v3/micro/code-go/worker_pool.go   # copied, not downloaded
    dest: code-go/worker_pool.go
```

`build-corpus.sh --pilot <dest>` reads `src`/`dest`; `build-corpus.sh --frozen
<dest> --manifest <frozen.yaml>` reads `url`/`sha256`/`dest`. The two paths share
one layout step so the pilot and frozen corpora are structurally interchangeable
to the indexer and harness.

## 5. Authoring rules (freeze hygiene — D11)

1. **Pin immutably.** Every `url` must reference content that cannot change under
   it; every `sha256` must match the exact bytes. A mutable `url` is a schema
   violation even if the `sha256` currently matches.
2. **One id = one content.** Never repoint an `id` at different bytes. New
   content gets a new `id`.
3. **License before inclusion.** No entry without a redistributable `license`.
4. **Order is layout.** Reordering `entries` is a no-op for the tree but changes
   build logs; keep order stable across revisions to make manifest diffs readable.
5. **Freeze before scoring.** Once the corpus + query set + thresholds are
   git-tagged `bench-v1-frozen`, the manifest is immutable for that run. Any edit
   (new entry, changed `sha256`, changed `dest`) voids the run and requires a new
   tag. See `docs/bench-methodology.md` §D11.
