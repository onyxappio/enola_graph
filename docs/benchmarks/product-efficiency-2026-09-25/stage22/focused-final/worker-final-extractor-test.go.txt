package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// A minified file is skipped by the parser but is not necessarily silent: a
// generated gRPC stub or a bundled GraphQL server still declares services and
// documents that the framework index has to see, and that other files'
// extraction depends on. Before minified records carried that summary the only
// way to see it was to read the bundle again on every run, and the cached-record
// branch of the composition signature saw nothing at all. These tests cover both
// halves: what the record must carry across a persistence round trip, and what
// the signature must refuse to trust.

// minifiedPad is long enough that isMinifiedSource classifies whatever it is
// appended to, without changing that source's framework contribution.
var minifiedPad = "\nexport const pad = [" + strings.Repeat("0,", minifiedLineThreshold) + "0];\n"

func minify(t *testing.T, src string) string {
	t.Helper()
	out := src + minifiedPad
	if !isMinifiedSource([]byte(out)) {
		t.Fatal("fixture is not classified minified; the padding no longer works")
	}
	return out
}

func frameworkRepo(t *testing.T, files map[string]string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	names := make([]string, 0, len(files))
	for rel, body := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, rel)
	}
	sort.Strings(names)
	return root, names
}

// persisted round-trips records through JSON the way the graph session stores
// and reloads them, so a reuse test proves the persisted form carries the
// summary rather than an in-memory pointer doing it.
func persisted(t *testing.T, recs map[string]*FileRecord) map[string]*FileRecord {
	t.Helper()
	raw, err := json.Marshal(recs)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*FileRecord{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != len(recs) {
		t.Fatalf("round trip lost records: %d -> %d", len(recs), len(out))
	}
	return out
}

func signature(t *testing.T, root string, names []string, prev map[string]*FileRecord, dirty map[string]bool) string {
	t.Helper()
	sig, err := CompositionSignature(root, names, prev, dirty, nil)
	if err != nil {
		t.Fatalf("composition signature: %v", err)
	}
	if sig == "" {
		t.Fatal("fixture produced an empty signature")
	}
	return sig
}

// canonicalFacts renders a whole fact set, every field, in a stable order. A
// projection onto kind and name could hide a role or a prop that a lost
// framework summary changed, so the comparison is over the marshalled facts.
func canonicalFacts(t *testing.T, ff []facts.Fact) string {
	t.Helper()
	rows := make([]string, 0, len(ff))
	for _, f := range ff {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, string(b))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func sameFacts(t *testing.T, what string, cold, warm []facts.Fact) {
	t.Helper()
	if len(cold) == 0 {
		t.Fatalf("%s: the cold run produced no facts, so equality would be vacuous", what)
	}
	if a, b := canonicalFacts(t, cold), canonicalFacts(t, warm); a != b {
		t.Fatalf("%s: reused facts differ from cold\n cold=%s\n warm=%s", what, a, b)
	}
}

// The record a skipped minified file leaves behind must carry the contribution
// collected from its bytes, and must not claim anything about a parse that did
// not happen.
func TestMinifiedRecordCarriesFrameworkSummary(t *testing.T) {
	rel := "gen/users/v1/users.client.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel:           minify(t, genUsersClient),
		"src/util.ts": "export function realHelper() {\n  return 1;\n}\n",
	})

	res, err := New().ExtractSession(context.Background(), root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := res.Records[rel]
	if rec == nil {
		t.Fatal("no record for the minified stub")
	}
	if !rec.Minified {
		t.Fatal("the stub must still be recorded as minified")
	}
	if rec.GRPC == nil || len(rec.GRPC.Methods) == 0 {
		t.Fatalf("minified record lost the gRPC contribution: %+v", rec.GRPC)
	}
	if rec.NuxtScope == "" {
		t.Fatal("minified record must carry a valid scope; an empty one is the legacy marker")
	}
	// Nothing about the parse is claimed. These are what keep the conservative
	// dependent-parse gates conservative for a file nobody resolved.
	if rec.ImportComplete {
		t.Fatal("a skipped file must not claim complete imports")
	}
	if len(rec.Facts) != 0 {
		t.Fatalf("a skipped file must contribute no facts, got %d", len(rec.Facts))
	}
	if rec.Hash == "" {
		t.Fatal("the record must keep its hash; it is what the fences re-prove")
	}

	// The persisted form is what a later run actually reads.
	back := persisted(t, res.Records)[rel]
	if back == nil || back.GRPC == nil || len(back.GRPC.Methods) != len(rec.GRPC.Methods) || back.NuxtScope != rec.NuxtScope {
		t.Fatalf("the contribution did not survive persistence: %+v", back)
	}
}

// Both halves of the GraphQL contribution are asserted separately: the server
// signal alone is not the SDL, and the SDL alone is not the server. GraphQLSDL
// holds the paths of the documents the file imports, not inline template text,
// so the fixture has to import a real document for the SDL half to say anything.
func TestMinifiedGraphQLProducerRecordCarriesServerAndSDL(t *testing.T) {
	producer, document := "src/server.ts", "src/schema.graphql"
	server := "import schema from \"./schema.graphql\";\nbuildSchema(schema);\n"
	root, names := frameworkRepo(t, map[string]string{
		producer: minify(t, server),
		document: "type Query {\n  real: String\n}\n",
	})

	// What the collector finds in these bytes, before any equality claim.
	want := collectGraphQLContribution(producer, []byte(minify(t, server)))
	if !want.Server {
		t.Fatal("fixture is not recognized as a GraphQL server")
	}
	if len(want.SDL) != 1 || want.SDL[0] != document {
		t.Fatalf("fixture imports no SDL document, so the SDL half would be vacuous: %v", want.SDL)
	}

	res, err := New().ExtractSession(context.Background(), root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := persisted(t, res.Records)[producer]
	if rec == nil || !rec.Minified {
		t.Fatalf("expected a minified record, got %+v", rec)
	}
	if !rec.GraphQLServer {
		t.Fatal("minified record lost the GraphQL server signal")
	}
	if len(rec.GraphQLSDL) != 1 || rec.GraphQLSDL[0] != document {
		t.Fatalf("minified record lost the imported SDL document: got %v want [%s]", rec.GraphQLSDL, document)
	}
}

// The signature computed from persisted records must equal the one computed from
// the bytes. This is the property the cached-record branch exists to have.
func TestCompositionSignatureCachedEqualsColdForMinifiedGRPCProducer(t *testing.T) {
	rel := "gen/users/v1/users.client.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel:           minify(t, genUsersClient),
		"src/util.ts": "export function realHelper() {\n  return 1;\n}\n",
	})

	res, err := New().ExtractSession(context.Background(), root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if rec := res.Records[rel]; rec == nil || rec.GRPC == nil {
		t.Fatalf("fixture did not produce a minified gRPC contribution: %+v", rec)
	}

	cold := signature(t, root, names, nil, nil)
	cached := signature(t, root, names, persisted(t, res.Records), map[string]bool{})
	if cold != cached {
		t.Fatalf("cached signature differs from cold:\n cold  =%s\n cached=%s", cold, cached)
	}
}

func TestCompositionSignatureCachedEqualsColdForMinifiedGraphQLProducer(t *testing.T) {
	producer := "src/server.ts"
	root, names := frameworkRepo(t, map[string]string{
		producer:             minify(t, "import schema from \"./schema.graphql\";\nbuildSchema(schema);\n"),
		"src/schema.graphql": "type Query {\n  real: String\n}\n",
	})

	res, err := New().ExtractSession(context.Background(), root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if rec := res.Records[producer]; rec == nil || !rec.GraphQLServer || len(rec.GraphQLSDL) != 1 {
		t.Fatalf("fixture did not produce a minified GraphQL contribution: %+v", rec)
	}

	cold := signature(t, root, names, nil, nil)
	cached := signature(t, root, names, persisted(t, res.Records), map[string]bool{})
	if cold != cached {
		t.Fatalf("cached signature differs from cold:\n cold  =%s\n cached=%s", cold, cached)
	}
}

// A record written before minified files carried a summary is empty, and
// consuming it as an empty contribution silently drops the bundle's services.
// The signature collector must re-read such a record instead. This is the shape
// of the mismatch reproduced on main before the change.
func TestCompositionSignatureRereadsLegacyMinifiedRecord(t *testing.T) {
	rel := "gen/users/v1/users.client.ts"
	bundle := minify(t, genUsersClient)
	root, names := frameworkRepo(t, map[string]string{rel: bundle})
	if c := grpcFileContribution([]byte(bundle)); c == nil || len(c.Methods) == 0 {
		t.Fatal("fixture lacks a gRPC contribution, so the test would pass vacuously")
	}

	cold := signature(t, root, names, nil, nil)
	legacy := map[string]*FileRecord{rel: {File: rel, Minified: true}}
	cached := signature(t, root, names, persisted(t, legacy), map[string]bool{})
	if cold != cached {
		t.Fatalf("a legacy minified record was consumed as empty:\n cold  =%s\n cached=%s", cold, cached)
	}
}

// Ambiguity is a property of two contributions colliding, not of a field on one
// record. Two minified stubs declaring one service name under different
// fully-qualified names must collide identically whether the collector reads the
// bytes or merges the persisted records, and the unrelated service named once
// must keep resolving either way.
func TestCompositionSignatureMinifiedAmbiguityMatchesCold(t *testing.T) {
	first, second, third := "gen/v1/dra.ts", "gen/v1beta1/dra.ts", "gen/v1/node.ts"
	root, names := frameworkRepo(t, map[string]string{
		first:  minify(t, draV1Stub),
		second: minify(t, draV1beta1Stub),
		third:  minify(t, nodeStub),
	})

	// The collision has to be real, and something has to survive it, or cached
	// and cold would agree for the uninteresting reason that the index is nil.
	idx := stubIndex(t,
		[2]string{first, draV1Stub},
		[2]string{second, draV1beta1Stub},
		[2]string{third, nodeStub})
	if idx == nil {
		t.Fatal("fixture collapsed the whole index; keep a service that resolves")
	}
	if !idx.ambiguousService["DRAPlugin"] {
		t.Fatalf("fixture does not collide: %v", idx.byService)
	}
	if svc := idx.byService["NodeService"]; svc == nil {
		t.Fatalf("the independent service must survive the collision: %v", idx.byService)
	}

	res, err := New().ExtractSession(context.Background(), root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{first, second, third} {
		rec := res.Records[rel]
		if rec == nil || !rec.Minified || rec.GRPC == nil {
			t.Fatalf("%s did not produce a minified record with a contribution: %+v", rel, rec)
		}
	}

	cold := signature(t, root, names, nil, nil)
	cached := signature(t, root, names, persisted(t, res.Records), map[string]bool{})
	if cold != cached {
		t.Fatalf("colliding minified stubs signed differently cached and cold:\n cold  =%s\n cached=%s", cold, cached)
	}
}

// The consumer statement for gRPC. The route below exists only because the
// minified client stub declared UserServiceClient: with the stub's summary lost,
// the call resolves to nothing and the route disappears, so this equality is not
// vacuous. Only the consumer is dirty, so the stub is reused from the persisted
// record rather than re-read.
func TestMinifiedGRPCStubStillResolvesConsumerAfterReuse(t *testing.T) {
	stub, consumer := "gen/users/v1/users.client.ts", "app.ts"
	route := "/users.v1.UserService/CreateUser"
	app := "import { UserServiceClient } from \"./gen/users/v1/users.client\";\n" +
		"const userService = new UserServiceClient(makeTransport());\n" +
		"async function submit(name: string) { return await userService.createUser({ name }); }\n"
	root, names := frameworkRepo(t, map[string]string{
		"gen/users/v1/users.ts": genUsersService,
		stub:                    minify(t, genUsersClient),
		consumer:                app,
	})
	ext := New()
	ctx := context.Background()

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if rec := cold.Records[stub]; rec == nil || !rec.Minified || rec.GRPC == nil {
		t.Fatalf("the fixture stub is not a minified gRPC producer: %+v", rec)
	}
	if _, ok := hasGRPCRoute(cold.Facts, route); !ok {
		t.Fatalf("the cold run did not resolve the consumer call; the fixture is wrong: %s", canonicalFacts(t, cold.Facts))
	}

	warm, err := ext.ExtractSession(ctx, root, names, persisted(t, cold.Records),
		map[string]bool{consumer: true}, SessionHooks{GraphPlannerOwnsInvalidation: true})
	if err != nil {
		t.Fatal(err)
	}
	if warm.Stats.FilesRead != 1 {
		t.Fatalf("expected only the consumer to be read, got %d files", warm.Stats.FilesRead)
	}
	if _, ok := hasGRPCRoute(warm.Facts, route); !ok {
		t.Fatalf("the reused minified stub lost the consumer route %s", route)
	}
	sameFacts(t, "minified grpc stub reuse", cold.Facts, warm.Facts)
}

// The consumer statement for GraphQL. A schema document is read as server-side
// schema only while a GraphQL server is known, and here the only server is the
// minified bundle. A lost GraphQLServer flag changes how the document extracts,
// which is what the route assertion and the fact comparison catch.
func TestMinifiedGraphQLServerStillShapesConsumerAfterReuse(t *testing.T) {
	producer, document := "server.ts", "schema.graphql"
	root, names := frameworkRepo(t, map[string]string{
		producer: minify(t, "import schema from \"./schema.graphql\";\nbuildSchema(schema);\n"),
		document: "type Query {\n  real: String\n}\n",
	})
	ext := New()
	ctx := context.Background()

	hasQuery := func(ff []facts.Fact) bool {
		for _, f := range ff {
			if f.Kind == facts.KindRoute && f.Name == "Query.real" && f.File == document {
				return true
			}
		}
		return false
	}

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if rec := cold.Records[producer]; rec == nil || !rec.Minified || !rec.GraphQLServer {
		t.Fatalf("the fixture producer is not a minified GraphQL server: %+v", rec)
	}
	if !hasQuery(cold.Facts) {
		t.Fatalf("the cold run produced no server-side schema route; the fixture is wrong: %s", canonicalFacts(t, cold.Facts))
	}

	warm, err := ext.ExtractSession(ctx, root, names, persisted(t, cold.Records),
		map[string]bool{document: true}, SessionHooks{GraphPlannerOwnsInvalidation: true})
	if err != nil {
		t.Fatal(err)
	}
	if warm.Stats.FilesRead != 1 {
		t.Fatalf("expected only the document to be read, got %d files", warm.Stats.FilesRead)
	}
	if !hasQuery(warm.Facts) {
		t.Fatal("the reused minified server lost the server-side schema route")
	}
	sameFacts(t, "minified graphql server reuse", cold.Facts, warm.Facts)
}

// The point of the record change: the second extract must not read the bundle
// again, and must still carry its contribution forward.
func TestMinifiedRecordIsNotRereadOnUnchangedSecondExtract(t *testing.T) {
	rel := "gen/users/v1/users.client.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel:           minify(t, genUsersClient),
		"src/util.ts": "export function realHelper() {\n  return 1;\n}\n",
	})
	ext := New()
	ctx := context.Background()

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := ext.ExtractSession(ctx, root, names, persisted(t, cold.Records), map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if warm.Stats.FilesRead != 0 {
		t.Fatalf("an unchanged delta read %d files; the minified bundle is still being re-read", warm.Stats.FilesRead)
	}
	rec := warm.Records[rel]
	if rec == nil || rec.GRPC == nil || len(rec.GRPC.Methods) == 0 {
		t.Fatalf("the reused record lost its contribution: %+v", rec)
	}
}

// A legacy record refreshes itself through need(), once, and then stops being
// read. No migration step runs.
func TestLegacyMinifiedRecordRefreshesOnceThenIsSkipped(t *testing.T) {
	rel := "gen/users/v1/users.client.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel: minify(t, genUsersClient),
	})
	ext := New()
	ctx := context.Background()

	legacy := map[string]*FileRecord{rel: {File: rel, Minified: true}}
	refreshed, err := ext.ExtractSession(ctx, root, names, persisted(t, legacy), map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Stats.FilesRead == 0 {
		t.Fatal("a legacy minified record was not refreshed")
	}
	rec := refreshed.Records[rel]
	if rec == nil || rec.GRPC == nil || rec.NuxtScope == "" {
		t.Fatalf("the refreshed record is still incomplete: %+v", rec)
	}

	settled, err := ext.ExtractSession(ctx, root, names, persisted(t, refreshed.Records), map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if settled.Stats.FilesRead != 0 {
		t.Fatalf("the refreshed record was read again: %d files", settled.Stats.FilesRead)
	}
}

// Changed bytes are still read and the contribution is recomputed, not carried
// over. The dirty set is what proves the bytes moved; the record's own summary
// never outlives them.
func TestMinifiedRecordRereadWhenBytesChange(t *testing.T) {
	rel := "gen/dra.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel: minify(t, draV1Stub),
	})
	ext := New()
	ctx := context.Background()

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	before := cold.Records[rel]
	if before == nil || before.GRPC == nil || before.GRPC.FQ == "" {
		t.Fatalf("fixture produced no service: %+v", before)
	}

	if err := os.WriteFile(filepath.Join(root, rel), []byte(minify(t, draV1beta1Stub)), 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := ext.ExtractSession(ctx, root, names, persisted(t, cold.Records), map[string]bool{rel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	after := next.Records[rel]
	if after == nil || after.GRPC == nil {
		t.Fatalf("the changed bundle produced no contribution: %+v", after)
	}
	if after.GRPC.FQ == before.GRPC.FQ {
		t.Fatalf("the stale contribution survived a byte change: %s", after.GRPC.FQ)
	}
	if after.Hash == before.Hash {
		t.Fatal("the record hash did not move with the bytes")
	}
}

// Both transitions across the minified boundary. A file that stops being
// minified must be parsed and produce facts; one that starts must stop.
func TestMinifiedBoundaryTransitionsBothWays(t *testing.T) {
	rel := "src/lib.ts"
	plain := "export function realHelper() {\n  return 1;\n}\n"
	root, names := frameworkRepo(t, map[string]string{rel: plain})
	ext := New()
	ctx := context.Background()

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if rec := cold.Records[rel]; rec == nil || rec.Minified || len(rec.Facts) == 0 {
		t.Fatalf("a plain source must parse into facts: %+v", rec)
	}

	if err := os.WriteFile(filepath.Join(root, rel), []byte(minify(t, plain)), 0o600); err != nil {
		t.Fatal(err)
	}
	became, err := ext.ExtractSession(ctx, root, names, persisted(t, cold.Records), map[string]bool{rel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := became.Records[rel]
	if rec == nil || !rec.Minified {
		t.Fatalf("the file must now be minified: %+v", rec)
	}
	if len(rec.Facts) != 0 || rec.ImportComplete {
		t.Fatalf("a newly minified file must drop its facts and its completeness: facts=%d complete=%v", len(rec.Facts), rec.ImportComplete)
	}

	if err := os.WriteFile(filepath.Join(root, rel), []byte(plain), 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ext.ExtractSession(ctx, root, names, persisted(t, became.Records), map[string]bool{rel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	restored := back.Records[rel]
	if restored == nil || restored.Minified || len(restored.Facts) == 0 {
		t.Fatalf("a file that stopped being minified must parse again: %+v", restored)
	}
}

// A unit statement about need(), not about a real Nuxt layout move: the stored
// scope is edited to one the layout does not produce, which is the shape a moved
// layout would leave behind. The end-to-end statement, with the config actually
// changing, belongs to the graph-session tests.
func TestMinifiedRecordRereadWhenStoredScopeDoesNotMatchLayout(t *testing.T) {
	rel := "apps/web/gen/dra.ts"
	root, names := frameworkRepo(t, map[string]string{
		rel:                       minify(t, draV1Stub),
		"apps/web/nuxt.config.ts": "export default defineNuxtConfig({})\n",
		"apps/web/package.json":   "{\"name\":\"web\",\"dependencies\":{\"nuxt\":\"^3\"}}",
	})
	ext := New()
	ctx := context.Background()

	cold, err := ext.ExtractSession(ctx, root, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := cold.Records[rel]
	if rec == nil || !rec.Minified || rec.NuxtScope == "" {
		t.Fatalf("expected a scoped minified record: %+v", rec)
	}
	scoped := rec.NuxtScope

	moved := persisted(t, cold.Records)
	stale := *moved[rel]
	stale.NuxtScope = scoped + "-moved"
	moved[rel] = &stale

	next, err := ext.ExtractSession(ctx, root, names, moved, map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if next.Stats.FilesRead == 0 {
		t.Fatal("a minified record whose stored scope no longer matches was not re-read")
	}
	if got := next.Records[rel]; got == nil || got.NuxtScope != scoped {
		t.Fatalf("the scope was not recomputed: %+v", got)
	}
}
