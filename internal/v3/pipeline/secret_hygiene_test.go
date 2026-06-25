//go:build cgo

package pipeline

import (
	"context"
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

	// Negative: each secret fragment must be absent from chunks.text.
	for _, frag := range []struct{ name, val string }{
		{"AWS key", fakeAWSKey},
		{"GitHub token", fakeGHToken},
		{"JWT", fakeJWT},
		{"PEM header", pemBeginLine},
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
