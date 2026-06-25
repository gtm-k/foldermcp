//go:build cgo

package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/walker"
)

// Phase 7 (D17) — INGEST secret-redaction gating tests. A secret planted in a
// fixture must be UNFINDABLE: absent from chunks.text, absent from the
// chunks_fts index, and the [REDACTED] marker present in the affected chunk.
// These assert at the DB layer (no live daemon/model needed).

// secretFragments are fake (non-functional) credentials that the sandbox
// pattern list + the runner's JWT pattern must redact at ingest. Each value is
// synthetic — never a real key.
const (
	fakeAWSKey    = "AKIAIOSFODNN7EXAMPLE"                                                                                     // AWS access key id (AKIA + 16)
	fakeGHToken   = "ghp_0123456789abcdefghijABCDEFGHIJ012345"                                                                 // GitHub PAT (ghp_ + 20+)
	fakeJWT       = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dQw4w9WgXcQabcdefghijklmnopqrstuvwxyz12" // 3-segment JWT
	pemBeginLine  = "-----BEGIN RSA PRIVATE KEY-----"                                                                          // PEM header
	pemBodyLine   = "MIIEpAIBAAKCAQEA1234567890abcdefGHIJKLqrstuvwxyzZZZZ"                                                     // (fake) PEM body
	pemEndLine    = "-----END RSA PRIVATE KEY-----"
	gitSHAControl = "9f1c3a2b7d4e5f60718293a4b5c6d7e8f9012345" // 40-char hex git SHA — must NOT be altered at ingest
)

// secretMarkdown is a prose fixture whose chunk text carries every planted
// secret. It is large enough to chunk but the secrets sit in one chunk.
const secretMarkdown = "# Deployment notes\n\n" +
	"Set the AWS access key " + fakeAWSKey + " in the environment.\n" +
	"The CI token is " + fakeGHToken + " for pushes.\n" +
	"Bearer " + fakeJWT + " authenticates the API.\n" +
	pemBeginLine + "\n" + pemBodyLine + "\n" + pemEndLine + "\n\n" +
	"The release commit is " + gitSHAControl + " (do not redact this hash).\n"

// TestIngestRedaction_SecretsUnfindable plants fake AWS/GitHub/JWT/PEM secrets
// in a prose fixture, indexes it, and asserts none of the secret fragments
// survive in chunks.text or chunks_fts, while the [REDACTED] marker IS present.
func TestIngestRedaction_SecretsUnfindable(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"deploy.md": secretMarkdown})
	r := newTestRunner(t, db)

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM chunks`); got == 0 {
		t.Fatal("no chunks indexed — fixture produced nothing to assert on")
	}

	// Negative: each secret fragment must be absent from chunks.text. The PEM
	// BODY and END line (HIGH-2) are checked alongside the header line — the body
	// IS the secret, so masking only the BEGIN line is a leak.
	for _, frag := range []struct{ name, val string }{
		{"AWS key", fakeAWSKey},
		{"GitHub token", fakeGHToken},
		{"JWT", fakeJWT},
		{"PEM header", pemBeginLine},
		{"PEM body", pemBodyLine},
		{"PEM footer", pemEndLine},
	} {
		if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE ?`, "%"+frag.val+"%"); got != 0 {
			t.Errorf("%s findable in chunks.text (%d rows) — ingest redaction failed", frag.name, got)
		}
		// Negative: absent from the FTS index too. FTS MATCH tokenizes, so probe
		// with a LIKE over the FTS shadow text as well as a MATCH on a token.
		if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE text LIKE ?`, "%"+frag.val+"%"); got != 0 {
			t.Errorf("%s findable in chunks_fts (%d rows) — FTS sees unredacted text", frag.name, got)
		}
	}

	// LOW-8: a tokenized chunks_fts MATCH probe on a secret-derived token. The
	// AWS key and GitHub token each tokenize to terms; a MATCH on them must return
	// nothing now that the secret is redacted before the FTS sync trigger fires.
	for _, tok := range []string{fakeAWSKey, fakeGHToken} {
		if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?`, tok); got != 0 {
			t.Errorf("secret token %q findable via chunks_fts MATCH (%d rows) — FTS index leaks the secret", tok, got)
		}
	}

	// The marker IS present in the affected chunk(s).
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%[REDACTED]%'`); got == 0 {
		t.Error("[REDACTED] marker absent — secrets were dropped without an observable marker")
	}

	// Negative CONTROL (D17): the 40-char git SHA must survive ingest untouched —
	// proves the long-token heuristic is OFF at ingest.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE ?`, "%"+gitSHAControl+"%"); got == 0 {
		t.Errorf("git SHA control %q was altered at ingest — long-token heuristic must not run at ingest (D17)", gitSHAControl)
	}

	// The redaction counter incremented (observability).
	if r.chunksRedactedTotal == 0 {
		t.Error("chunksRedactedTotal = 0, want > 0 — redaction counter not surfaced")
	}
}

// TestIngestRedaction_GitSHAControlUnaltered isolates the D17 negative control:
// a chunk whose ONLY notable token is a 40-char git SHA (no secret patterns) is
// indexed verbatim — no [REDACTED] marker, SHA findable.
func TestIngestRedaction_GitSHAControlUnaltered(t *testing.T) {
	db := openTestDB(t)
	body := "# Changelog\n\nReverted in commit " + gitSHAControl + " after the regression.\n" +
		"See also base64 blob YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXowMTIzNDU2Nzg5QUJD',\n"
	dir := seedDir(t, map[string]string{"changelog.md": body})
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE ?`, "%"+gitSHAControl+"%"); got == 0 {
		t.Errorf("git SHA %q not findable — ingest must not redact it (D17 control)", gitSHAControl)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%[REDACTED]%'`); got != 0 {
		t.Errorf("[REDACTED] present in a SHA-only fixture (%d) — long-token heuristic leaked into ingest", got)
	}
	if r.chunksRedactedTotal != 0 {
		t.Errorf("chunksRedactedTotal = %d, want 0 — no secret present", r.chunksRedactedTotal)
	}
}

// TestWalkerExcludesSecretFiles asserts Layer 1: an .env file (and other
// credential-bearing files) never enter the files table.
func TestWalkerExcludesSecretFiles(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		"app.go":              "package app\n",
		".env":                "AWS_SECRET_ACCESS_KEY=" + fakeAWSKey + "\n",
		".env.local":          "TOKEN=" + fakeGHToken + "\n",
		"server.pem":          pemBeginLine + "\n" + pemBodyLine + "\n" + pemEndLine + "\n",
		"deploy.key":          "ssh-rsa AAAA...\n",
		"id_rsa":              "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		"my-credentials.json": "{\"token\":\"" + fakeGHToken + "\"}\n",
		"vault.keystore":      "binary-ish\n",
	})
	if _, err := walker.Walk(context.Background(), db, walker.Options{Root: dir}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, name := range []string{".env", ".env.local", "server.pem", "deploy.key", "id_rsa", "my-credentials.json", "vault.keystore"} {
		if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path LIKE ?`, "%"+name); got != 0 {
			t.Errorf("secret file %q entered files table (%d rows) — Layer 1 deny failed", name, got)
		}
	}
	// The non-secret file IS indexed.
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path LIKE ?`, "%app.go"); got != 1 {
		t.Errorf("app.go indexed = %d, want 1", got)
	}
}

// TestIngestRedaction_HeaderSecretUnfindable (HIGH-1): chunks_fts indexes BOTH
// text AND header. A secret planted in a CSV COLUMN NAME flows into the
// csv_schema chunk's header (buildSchemaHeader: "columns: <cols> — <title>"). If
// the header is inserted RAW, the secret is FINDABLE via an FTS MATCH/LIKE on the
// header column even though chunk text is redacted. This asserts the column-name
// secret is absent from chunks.header AND from a chunks_fts probe, with the
// [REDACTED] marker present.
func TestIngestRedaction_HeaderSecretUnfindable(t *testing.T) {
	db := openTestDB(t)
	// AWS key as a CSV column name. It is alphanumeric/non-numeric, so the CSV
	// chunker treats the first row as a real header (looksLikeHeader) and builds
	// the schema header from these column names.
	var csvb strings.Builder
	csvb.WriteString("id," + fakeAWSKey + ",price\n")
	for i := 0; i < 20; i++ {
		csvb.WriteString("1,widget,9.99\n")
	}
	dir := seedDir(t, map[string]string{"products.csv": csvb.String()})
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE chunk_kind='csv_schema'`); got == 0 {
		t.Fatal("no csv_schema chunk indexed — fixture produced no header to assert on")
	}

	// Negative: the column-name secret must be absent from chunks.header.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE header LIKE ?`, "%"+fakeAWSKey+"%"); got != 0 {
		t.Errorf("secret findable in chunks.header (%d rows) — header ingest redaction failed", got)
	}
	// Negative: absent from the FTS header column (LIKE over the shadow text).
	if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE header LIKE ?`, "%"+fakeAWSKey+"%"); got != 0 {
		t.Errorf("secret findable in chunks_fts.header (%d rows) — FTS sees unredacted header", got)
	}
	// Negative (FTS MATCH probe, LOW-8): a tokenized MATCH on the secret token over
	// the header column must return nothing. The AWS key tokenizes to a single
	// term; column='header' scopes the MATCH to the header column.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?`, "header:"+fakeAWSKey); got != 0 {
		t.Errorf("secret findable via chunks_fts MATCH on header (%d rows) — FTS index leaks the secret", got)
	}
	// The [REDACTED] marker IS present in the header (observable redaction).
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE header LIKE '%[REDACTED]%'`); got == 0 {
		t.Error("[REDACTED] absent from chunks.header — header secret dropped without a marker")
	}
}

// TestRedactIngestHeader_ProseHeading (HIGH-1, unit): the header-redaction guard
// must scrub a secret planted in a PROSE HEADING just as it does a CSV column
// name. The recursive prose chunker does not currently populate Header in the
// pipeline, so this asserts the guard (redactIngestHeader) directly — the single
// function every chunk-insert site routes its header through. A nil header (the
// code-AST branch) must pass through untouched.
func TestRedactIngestHeader_ProseHeading(t *testing.T) {
	r := &Runner{}
	heading := "API key " + fakeGHToken + " setup"
	got, _ := r.redactIngestHeader(heading).(string)
	if strings.Contains(got, fakeGHToken) {
		t.Errorf("redactIngestHeader left secret in prose heading: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("redactIngestHeader produced no [REDACTED] marker: %q", got)
	}
	if r.redactIngestHeader(nil) != nil {
		t.Error("redactIngestHeader(nil) must stay nil (NULL header column)")
	}
	if r.redactIngestHeader("") != any("") {
		t.Error("redactIngestHeader(\"\") must stay empty")
	}
}

// TestIngestRedaction_PEMBodyAbsent (HIGH-2): the ingest layer must redact the
// FULL PEM private-key block — the BEGIN line, the base64 BODY (which IS the
// secret), AND the END line — not just the header line. Plants a PEM block in an
// indexed .md and asserts all three are absent from chunks.text and chunks_fts.
func TestIngestRedaction_PEMBodyAbsent(t *testing.T) {
	db := openTestDB(t)
	body := "# Server key\n\n" +
		"Install the key below:\n" +
		pemBeginLine + "\n" +
		pemBodyLine + "\n" +
		"AAAABBBBCCCCDDDDeeeeffffgggghhhh11112222\n" + // a second body line
		pemEndLine + "\n\n" +
		"Then restart the service.\n"
	dir := seedDir(t, map[string]string{"keynotes.md": body})
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks`); got == 0 {
		t.Fatal("no chunks indexed")
	}

	for _, frag := range []struct{ name, val string }{
		{"PEM BEGIN line", pemBeginLine},
		{"PEM body line 1", pemBodyLine},
		{"PEM body line 2", "AAAABBBBCCCCDDDDeeeeffffgggghhhh11112222"},
		{"PEM END line", pemEndLine},
	} {
		if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE ?`, "%"+frag.val+"%"); got != 0 {
			t.Errorf("%s findable in chunks.text (%d rows) — PEM body/footer not redacted at ingest", frag.name, got)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE text LIKE ?`, "%"+frag.val+"%"); got != 0 {
			t.Errorf("%s findable in chunks_fts (%d rows) — FTS sees unredacted PEM", frag.name, got)
		}
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%[REDACTED]%'`); got == 0 {
		t.Error("[REDACTED] marker absent — PEM block dropped without a marker")
	}
}

// TestRedactIngest_PEMBlock (HIGH-2, unit): direct check of the multi-line PEM
// block redaction over the runner ingest layer — the entire block collapses to a
// single [REDACTED] and no body byte survives.
func TestRedactIngest_PEMBlock(t *testing.T) {
	r := &Runner{}
	in := "before\n" + pemBeginLine + "\n" + pemBodyLine + "\n" + pemEndLine + "\nafter\n"
	out := r.redactIngest(in)
	for _, frag := range []string{pemBeginLine, pemBodyLine, pemEndLine} {
		if strings.Contains(out, frag) {
			t.Errorf("PEM fragment %q survived redactIngest: %q", frag, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("no [REDACTED] marker after PEM redaction: %q", out)
	}
	// Surrounding prose is preserved.
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Errorf("PEM redaction ate surrounding prose: %q", out)
	}
}

// TestJWTPattern_ShortSegments (MED-4): the relaxed JWT pattern must match compact
// JWTs whose claims/signature segments are short or empty (empty-claims `e30`,
// unsecured JWS with an empty trailing signature) WITHOUT regressing the
// SHA-safety control — a 40-char git SHA and a bare base64 literal must NOT match.
func TestJWTPattern_ShortSegments(t *testing.T) {
	r := &Runner{}
	matchPositive := []struct{ name, jwt string }{
		{"empty-claims compact", "eyJhbGciOiJIUzI1NiJ9.e30.c2lnbmF0dXJl"},
		{"unsecured empty signature", "eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIn0."},
		{"full 3-segment", fakeJWT},
	}
	for _, c := range matchPositive {
		in := "Authorization: Bearer " + c.jwt + " end"
		out := r.redactIngest(in)
		if strings.Contains(out, c.jwt) {
			t.Errorf("%s: JWT %q not redacted: %q", c.name, c.jwt, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s: no [REDACTED] marker: %q", c.name, out)
		}
	}
	// SHA-safety negative controls: must survive ingest UNALTERED (no eyJ anchor /
	// no dot-delimited triple).
	negative := []struct{ name, val string }{
		{"40-char git SHA", gitSHAControl},
		{"bare base64 literal", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXowMTIzNDU2Nzg5QUJD"},
	}
	for _, c := range negative {
		in := "value " + c.val + " end"
		out := r.redactIngest(in)
		if !strings.Contains(out, c.val) {
			t.Errorf("%s: %q was redacted at ingest — JWT/SHA control regressed: %q", c.name, c.val, out)
		}
	}
}

// TestWalkerIncludeSecretsOverride: explicit config (IncludeSecrets) opts back
// in — the deny list is default-on but overridable.
func TestWalkerIncludeSecretsOverride(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{".env": "X=1\n"})
	if _, err := walker.Walk(context.Background(), db, walker.Options{Root: dir, IncludeSecrets: true}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path LIKE ?`, "%.env"); got != 1 {
		t.Errorf(".env with IncludeSecrets=true indexed = %d, want 1", got)
	}
}
