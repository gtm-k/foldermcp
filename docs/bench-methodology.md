# Retrieval Benchmark Methodology (bench-v1)

This is the published, reproducible methodology for the FolderMCP retrieval
benchmark: how the corpus is assembled, how baselines are run, how they are
scored, the kill-gate that decides whether hybrid search earns its complexity,
and the freeze discipline that keeps the numbers honest.

It covers two scopes that must not be conflated:

- **PILOT** — a tiny offline run that validates the benchmark *machinery*
  end-to-end (Phase 9 / 9a). Numbers here are not a quality verdict.
- **FROZEN** — the human-gated `>=1000`-file corpus and `>=100` user-approved
  query set that produces the real, citable measurement (Phase 9b/9c).

See [§7 PILOT vs FROZEN](#7-pilot-vs-frozen) for exactly what is validated now
vs. what still needs the human gate.

---

## 1. What we measure and why

The benchmark answers one question: **does hybrid (lexical + filename + vector,
fused with RRF — the harness `auto` mode) retrieve materially better than the
baselines a skeptic would reach for first?** The two skeptic baselines are:

- **agentic-grep** (harness baseline `agentic-grep`): an agent loop restricted to
  literal/regex search (the "just grep it" baseline), given a realistic multi-step
  budget (§5). Strong on exact tokens, weak when the query concept does not share
  vocabulary with the file.
- **raw-read** (harness baseline `raw-read`): the no-index token-cost ceiling. An
  index-less agent still has to *find* candidate files (it greps for them), but
  then reads the top candidates **wholesale** instead of relying on retrieved
  snippets — what you pay if you skip the retrieval index entirely. Only the
  model-ingested surface is counted (query in + full file contents out — for a
  PDF candidate, its full extracted text layer); the grep
  used to *pick* the files is discovery I/O and is excluded, exactly as hybrid's
  index-internal work is excluded, so the two surfaces are identical (§5).

Both baselines are produced together by `scripts/retrieval-harness.go` in a
single run whenever `-corpus <dir>` is supplied; there is **no** per-baseline
selector flag (§3).

Hybrid only earns its place if it beats grep on relevance *and* stays far under
raw-read on token cost. That trade-off is the kill-gate ([§4](#4-kill-gate)).

Metrics are computed at **file/path granularity** (a hit matches a judgment when
the judgment path is a substring of the indexed hit path). `SearchHit` carries
no line number on the wire, so file:line answers from any baseline are reduced
to file granularity for a fair comparison.

## 2. Strata

Every corpus file belongs to exactly one stratum, derived from its **extension**
(not the indexer's `content_class`, which merges Markdown and PDF). Strata:
`code`, `md`, `pdf`, `csv`. The full extension → stratum map and the rationale
are in [`scripts/bench/corpus-manifest.schema.md`](../scripts/bench/corpus-manifest.schema.md) §3.

Per-stratum reporting matters because the kill-gate has a **PDF-extraction**
sub-criterion: a corpus can pass aggregate recall while silently failing to
extract any PDF text (e.g. `pdftotext` missing). Strata make that visible.

## 3. Reproducible command sequence

Prerequisites (one-time): a cgo build of the daemon (`make v3-build` →
`bin/foldermcp-v3`), the embedding model fetched (`make v3-fetch-model` →
`internal/v3/embed/model/`), the ONNX runtime shared lib available, and
`pdftotext` (poppler) on `PATH` for the PDF stratum. **Run the end-to-end step
on Linux/macOS/WSL** — the harness dials a `unix:` socket, which on native
Windows is a named pipe (see project notes); index/serve under WSL.

```bash
# 0. From the repo root.
REPO="$(pwd)"
CORPUS=/tmp/bench-pilot            # pilot; for frozen use the bench-v1 corpus dir
BIN="$REPO/bin/foldermcp-v3"

# 1. Build the pilot corpus (OFFLINE — copies in-repo fixtures, deterministic).
bash scripts/bench/build-corpus.sh --pilot "$CORPUS"
#    (FROZEN instead: bash scripts/bench/build-corpus.sh --frozen "$CORPUS" \
#       --manifest scripts/bench/frozen-corpus-manifest.yaml --allow-network)

# 2. Isolated runtime so we never touch the user's default store.
export HOME=/tmp/bench-home
export FOLDERMCP_STORE=/tmp/bench-store
export FOLDERMCP_MODEL_DIR="$REPO/internal/v3/embed/model"   # else semantic is silently OFF
# export FOLDERMCP_ORT_LIB=/abs/path/to/libonnxruntime.so    # pin to avoid wrong-version PATH lib
mkdir -p "$HOME/.foldermcp/run" "$FOLDERMCP_STORE"
SOCK="$HOME/.foldermcp/run/serve.sock"

# 3. Index + serve in one resident process (indexes in background, then serves).
"$BIN" all-v3 "$CORPUS" > /tmp/bench-serve.log 2>&1 &
SERVE_PID=$!
#    Wait for the socket, then let the first index pass finish (model load +
#    tree-sitter + sqlite-vec are lazy; ~45s is enough for the pilot).
until [ -S "$SOCK" ]; do sleep 1; done
sleep 45

# 4. Run the harness ONCE and capture machine-readable JSON. The harness is a
#    multi-file package, so it is run as `./scripts` (not a single .go file).
#    `-corpus` enables BOTH filesystem baselines (agentic-grep + raw-read)
#    alongside the gRPC modes; `-json` writes the full schema. There is no
#    per-baseline selector flag — one run yields auto/lexical/filename/semantic
#    + agentic-grep + raw-read + the token-efficiency ratio the gate needs.
#    (`rg` must be on PATH; `pdftotext` too for the PDF stratum.)
go run -tags harness ./scripts \
    -labeled testdata/v3/bench/m2_pilot_labeled_queries.yaml \
    -socket "$SOCK" \
    -corpus "$CORPUS" \
    -json > /tmp/bench.json

# 5. Read the JSON: recall@5 per mode/stratum + token tallies feed the kill-gate.
kill "$SERVE_PID"
```

The harness emits, per baseline run: recall@k and NDCG@k per mode and per
stratum, the per-stratum PDF-extraction rate, and request/response token tallies.
The kill-gate ([§4](#4-kill-gate)) is computed from those JSON fields.

## 4. Kill-gate

The benchmark either **justifies hybrid search or kills it.** Verbatim criteria:

> **PASS** = recall@5(hybrid) >= 1.25x recall@5(grep)
>            AND PDF-stratum extraction >= 90%
>            AND tokens <= 0.1x raw-read

All three conditions must hold simultaneously:

1. **Relevance lift** — `recall@5(hybrid) >= 1.25 * recall@5(grep)`. Hybrid must
   beat the grep baseline by at least 25% on recall@5. (Hybrid = harness `auto`
   mode; grep = the `agentic-grep` baseline.)
2. **Extraction floor** — `PDF-stratum extraction >= 90%`. At least 90% of
   PDF-stratum files that should yield text must actually be indexed with
   extractable text. Guards against a silently broken extractor masquerading as
   "low PDF recall."
3. **Cost ceiling** — `tokens <= 0.1 * raw-read`. Hybrid's end-to-end token cost
   must be at most one-tenth of the raw-read baseline. If retrieval does not cut
   tokens by 10x, the index is not paying for itself.

A run that fails any condition is a **NO-GO** for shipping hybrid as the default
under the measured configuration. The gate is computed only against the FROZEN
corpus + FROZEN queries (the pilot run is a dry-run of the gate arithmetic, not a
verdict — see §7).

## 5. Baseline fairness (Decision D16)

> **D16 — Baseline fairness.** Every baseline (hybrid, agentic-grep, raw-read) is
> evaluated on the **same frozen corpus, the same frozen query set, the same k,
> the same relevance judgments, and the same metric definitions.** No baseline
> receives query-specific tuning, hand-picked results, or a corpus subset chosen
> to favor it. The agentic-grep baseline is given a realistic agent budget (multi-
> step search/refine), not a single naive `grep` invocation, so the comparison is
> against grep *used well*, not a strawman. Token accounting is measured
> identically across baselines (same tokenizer/approximation and the same
> request+response surface). Any change that advantages one baseline must be
> applied symmetrically to all, or the run is void.

Practical consequences:

- The query set is authored **before** any baseline is run and is not edited to
  flatter hybrid. The pilot set deliberately includes **cross-naming** queries
  (concept != identifier) where grep is structurally weak *and* plain code/md/csv
  queries where grep is structurally strong — so the comparison is balanced, not
  cherry-picked.
- `MaxResults`/k is identical for all modes. (Note the harness's `filename` mode
  internally requests `k/2` server-side; that is an artifact of that diagnostic
  mode and is documented, not a fairness lever for the gate, which compares
  hybrid vs. agentic-grep vs. raw-read.)
- **agentic-grep budget split.** The agent's ripgrep search loop and its PDF
  text-layer extraction (`pdftotext`) draw from **separate** per-query budgets,
  exposed as two explicit flags — `-call-budget` (rg search + confirm reads) and
  `-pdf-budget` (extraction) — each clamped to the D16 floor of 8. The worst-case
  per-query tool calls is their sum, by design, so a reviewer can read and bound
  each independently. A natural-language query that expands to
  many rg terms therefore can never exhaust the budget before a single PDF is
  extracted — extracting a PDF's text is a once-per-file operation an agent
  "using grep well" always performs, not a search-refinement step competing with
  synonym expansions. Within the PDF budget, PDFs are ordered **query-first**
  (those whose path matches a query term are extracted before the rest, remainder
  alphabetical), so a relevant target is never crowded out by an
  alphabetically-earlier negative fixture. This ordering is query-driven, never
  judgment-driven, so it advantages no baseline.
- **Location-independent grep.** ripgrep is invoked with `--no-ignore --hidden`
  so matches depend only on the corpus *content*, not on ambient `.gitignore` /
  `.ignore` files, the host's global git excludes (`core.excludesFile`), or
  hidden-file skipping. Without this the same corpus could score differently on
  two reviewers' machines — a D16 reproducibility break.
- **Identical token surface (hybrid vs. raw-read).** Both are counted over the
  same model-ingested surface: **query text in**, and the response payload out —
  `path+title+snippet` per hit for hybrid vs. **full file contents** for
  raw-read (for a PDF, its full extracted text layer — not skipped). Neither
  counts its *discovery* machinery: hybrid excludes the index's
  internal lexical/vector/RRF work, and raw-read excludes the grep + `pdftotext`
  it uses only to choose which files to read wholesale. (Earlier the raw-read
  denominator folded in ~one full-corpus rg scan per reformulated term plus every
  PDF's extracted text; that one-directional inflation flattered hybrid's cost
  ratio and is removed.) The agentic-grep baseline is *not* part of this gate
  surface — its token tally legitimately includes the grep output it ingests,
  because that is the real cost of an agent driving grep.
- **Matched denominators.** The token-efficiency ratio is computed over the
  **intersection** of queries that *both* `auto` and `raw-read` actually
  evaluated (keyed by query id). `auto` can drop a query on an RPC error and
  raw-read on a discovery error — independent failure modes — so summing totals
  over non-identical sets could silently flatter the ratio; the harness instead
  sums only common queries and warns on stderr when the sets differ.
- **Visible partial extraction.** A `pdftotext` failure or a PDF-discovery walk
  error is logged to stderr with the file path and counted; a silently-partial
  PDF stratum (e.g. one corrupt file dropping out of recall + token accounting)
  cannot masquerade as complete (actor-observability).

## 6. Pre-registration and freeze (Decision D11)

> **D11 — Pre-registration / freeze.** The corpus manifest, the labeled query set
> (with grades), and the kill-gate thresholds are committed and **git-tagged
> `bench-v1-frozen` BEFORE any scoring run.** Once tagged, all three are immutable
> for that benchmark run. **Any edit — adding/removing a corpus entry, changing a
> file's `sha256`, editing a query, regrading a judgment, or moving a threshold —
> VOIDS the run.** A changed input requires a new tag (`bench-v2-frozen`, ...) and
> a fresh scoring run. Scores are only citable if they were produced against the
> exact tagged inputs.

Why: pre-registration is what separates a benchmark from a demo. If the corpus or
grades can move after seeing results, the kill-gate is meaningless — you can
always edit your way to PASS. The `sha256` pins in the frozen manifest (schema
§2) are the technical enforcement: a silently-changed upstream file fails the
build instead of quietly altering the score.

Freeze checklist (run once, before the first FROZEN scoring run):

1. Frozen corpus manifest authored (url + sha256 + license per entry).
2. `>=100` queries with user-approved grades committed.
3. Kill-gate thresholds in this doc fixed (§4).
4. `git tag bench-v1-frozen` on the commit containing all three.
5. Only then run §3 against the frozen corpus and read out the gate.

## 7. PILOT vs FROZEN

This is the boundary between what is validated **now** and what still requires the
human gate. Do not cite pilot numbers as a benchmark result.

| Aspect            | PILOT (now — Phase 9/9a)                                   | FROZEN (Phase 9b/9c — human-gated)                          |
|-------------------|-----------------------------------------------------------|------------------------------------------------------------|
| Corpus            | 26 in-repo fixtures, copied offline (`build-corpus.sh --pilot`). 16 code / 5 md / 3 pdf / 2 data. | `>=1000` files, downloaded + sha256-verified (`--frozen`). Real licensed sources across all strata. |
| Queries           | 13 **AI-drafted, NOT user-approved** (`testdata/v3/bench/m2_pilot_labeled_queries.yaml`). | `>=100` queries with **user-approved** grades. |
| Grades            | Synthetic, drafted from fixture content. | Human-reviewed; author != judge enforced by policy. |
| Freeze (D11)      | Not tagged; pilot inputs may change freely. | `git tag bench-v1-frozen` before any scoring; edits void the run. |
| Kill-gate (§4)    | **Dry-run only** — validates the gate arithmetic and JSON plumbing end-to-end. | **Authoritative** — PASS/NO-GO decision on shipping hybrid. |
| What it proves    | The machinery works: corpus builds deterministically, daemon indexes all 4 strata, harness runs all baselines, JSON is well-formed, metrics + token tallies compute, the gate expression evaluates. | The actual retrieval-quality and cost verdict. |
| What it does NOT  | Prove hybrid is better/cheaper. 13 unapproved queries over 26 files are not a measurement. | (n/a) |

**Validated by the pilot:** corpus assembly is reproducible (same manifest →
byte-identical tree); the offline corpus spans all four strata; the labeled-query
schema is accepted by the harness; cross-naming queries exist to stress the
grep baseline; the build script's frozen/download path exists and is wired (it
refuses without `--allow-network` and without a frozen manifest).

**Still requires the human gate (out of pilot scope):** authoring the
`>=1000`-file frozen corpus manifest with pinned URLs + checksums + licenses;
collecting `>=100` user-approved graded queries; pre-registering and tagging
`bench-v1-frozen`; and running the authoritative kill-gate. Until those land, no
benchmark number from this harness is citable.

## 8. Artifacts

- `scripts/bench/corpus-manifest.schema.md` — frozen + pilot manifest schema.
- `scripts/bench/pilot-corpus-manifest.yaml` — the 26-file offline pilot manifest.
- `scripts/bench/build-corpus.sh` — `--pilot` (offline copy) and `--frozen`
  (download + sha256-verify) corpus builders.
- `testdata/v3/bench/m2_pilot_labeled_queries.yaml` — 13 AI-drafted pilot
  queries (NOT user-approved). Tracked in-repo so the pilot is reproducible from
  a clean clone.
- `scripts/retrieval-harness.go` — the pure-gRPC harness (modes, baselines,
  metrics, JSON). Owned separately; this doc documents its intended interface.
