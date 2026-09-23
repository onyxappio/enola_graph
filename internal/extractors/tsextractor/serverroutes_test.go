package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// clientRoutes indexes emitted client-role routes by path -> method, the counterpart
// of serverRoutes in decoratorroutes_test.go.
func clientRoutes(ff []facts.Fact) map[string]string {
	out := map[string]string{}
	for _, f := range ff {
		if f.Kind == facts.KindRoute && f.Props["role"] == "client" {
			out[f.Name] = f.Props["method"].(string)
		}
	}
	return out
}

// TestServerRoutes_ExpressApp covers the root case: routes registered on an
// application object are served at the path as written.
func TestServerRoutes_ExpressApp(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/healthcheck', healthCheckController);
app.post('/go/:name', proxyLink());
app.get('*', redirects());
`
	got := serverRoutes(extractTS(t, src, "server/index.js"))
	if got["/healthcheck"] != "GET" {
		t.Errorf("app.get: %+v", got)
	}
	if got["/go/:name"] != "POST" {
		t.Errorf("app.post with a param: %+v", got)
	}
	// A bare catch-all is a SPA fallback, not an endpoint. Indexing it would let it
	// match any client path at all.
	if _, found := got["/*"]; found {
		t.Errorf("wildcard route must not be emitted: %+v", got)
	}
}

// TestServerRoutes_UnmountedRouterEmitsNothing is the correctness rule this pass is
// built around. A sub-router's paths are FRAGMENTS: `router.post('/login')` in a
// routes module that index.js mounts at '/webhooks' really serves
// '/webhooks/login'. Emitting '/login' would be a WRONG fact, and a wrong path can
// false-match another repo's route — worse than silence. This is the dominant
// layout for a routes/ directory, so the rule matters.
func TestServerRoutes_UnmountedRouterEmitsNothing(t *testing.T) {
	src := `
const express = require('express');
const router = express.Router();

router.post('/login', async (req, res) => {});
router.get('/login', async (req, res) => {});

module.exports = router;
`
	if got := serverRoutes(extractTS(t, src, "server/routes/webhooks.js")); len(got) != 0 {
		t.Errorf("a router with no visible mount must emit nothing, got %+v", got)
	}
}

// TestServerRoutes_SameFileMountComposes covers the mount this pass DOES resolve:
// declared and mounted in the same file, so the prefix is known without a repo-wide
// pass.
func TestServerRoutes_SameFileMountComposes(t *testing.T) {
	src := `
const express = require('express');
const app = express();
const router = express.Router();

router.get('/login', handler);
router.post('/logout', handler);

app.use('/webhooks', router);
app.get('/healthcheck', handler);
`
	got := serverRoutes(extractTS(t, src, "server/index.js"))
	for path, want := range map[string]string{
		"/webhooks/login":  "GET",
		"/webhooks/logout": "POST",
		"/healthcheck":     "GET",
	} {
		if got[path] != want {
			t.Errorf("%s: want %s, got %+v", path, want, got)
		}
	}
	if _, found := got["/login"]; found {
		t.Errorf("unmounted path must not survive alongside the composed one: %+v", got)
	}
}

// TestServerRoutes_DoNotStealClientCalls is the sharpest risk in this change.
// `axios.get('/x')` and `router.get('/x')` are the same text; v141 already emits the
// former as a client call. Only a receiver BOUND to an app/router in this file may
// become a server route, and an unknown receiver must keep its v141 behaviour
// exactly — reclassifying one would move existing facts.
func TestServerRoutes_FastifyRouteObjectLiteral(t *testing.T) {
	src := `
import Fastify from 'fastify'
const app = Fastify()
app.route({
  url: '/graphql',
  method: ['GET', 'POST', 'OPTIONS'],
  handler: async (req, reply) => {}
})
app.get('/health', async () => {})
`
	ff := extractTS(t, src, "services/memory-provider/src/app.ts")
	got := serverRoutes(ff)
	if got["/graphql"] == "" {
		t.Fatalf("expected /graphql from app.route: %+v", got)
	}
	if got["/health"] != "GET" {
		t.Errorf("sibling verb route lost: %+v", got)
	}
}

func TestServerRoutes_ParameterShadowDoesNotInheritFactory(t *testing.T) {
	src := `
import Fastify from 'fastify'
import { FastifyInstance } from 'fastify'
const app = Fastify()
app.route({ url: '/server', method: 'GET', handler: () => 1 })
function shadow(app: any) {
  app.route({ url: '/shadow', method: 'GET', handler: () => 1 })
}
export function register(app: FastifyInstance) {
  app.route({ url: '/typed', method: 'GET', handler: () => 1 })
}
`
	ff := extractTS(t, src, "src/index.ts")
	got := serverRoutes(ff)
	if got["/server"] == "" {
		t.Fatalf("module Fastify receiver lost: %+v", got)
	}
	if got["/typed"] == "" {
		t.Fatalf("typed FastifyInstance receiver lost: %+v", got)
	}
	if _, ok := got["/shadow"]; ok {
		t.Fatalf("shadowed any parameter inherited factory: %+v", got)
	}
	if clientRoutes(ff)["/shadow"] == "" {
		t.Fatalf("shadowed route must remain a client call: %+v", clientRoutes(ff))
	}
}

func TestServerRoutes_RouteObjectDoesNotStealClient(t *testing.T) {
	src := `
import axios from "axios";
export async function load() {
  await axios.route({ url: '/graphql', method: 'POST' });
}
`
	ff := extractTS(t, src, "src/client.ts")
	if got := serverRoutes(ff); len(got) != 0 {
		t.Errorf("axios.route must not become a server route: %+v", got)
	}
}

func TestServerRoutes_DoNotStealClientCalls(t *testing.T) {
	src := `
import axios from "axios";
import { http } from "../lib/http";

export async function load() {
  await axios.get("/api/v2/slots/available");
  await http.post("/slots/reserve", body);
  await someUnknownThing.get("/unknown/receiver");
}
`
	ff := extractTS(t, src, "src/api/client.ts")
	client := clientRoutes(ff)
	for path, want := range map[string]string{
		"/api/v2/slots/available": "GET",
		"/slots/reserve":          "POST",
		"/unknown/receiver":       "GET",
	} {
		if client[path] != want {
			t.Errorf("%s must stay a CLIENT call: want %s, got %+v", path, want, client)
		}
	}
	if got := serverRoutes(ff); len(got) != 0 {
		t.Errorf("no server routes in a file with no app/router binding: %+v", got)
	}
}

// TestServerRoutes_NoDoubleEmission pins the other half of that boundary: a route
// registration must be emitted ONCE, as a server route, not also as an outbound
// client call by v141's receiver-agnostic pass.
func TestServerRoutes_NoDoubleEmission(t *testing.T) {
	src := `
const express = require('express');
const app = express();
app.get('/healthcheck', handler);
`
	ff := extractTS(t, src, "server/index.js")
	if got := serverRoutes(ff); got["/healthcheck"] != "GET" {
		t.Errorf("expected a server route: %+v", got)
	}
	if got := clientRoutes(ff); len(got) != 0 {
		t.Errorf("a route registration must not also be a client call: %+v", got)
	}
}

// TestServerRoutes_OtherFrameworks covers the rest of the family that shares the
// shape, so the framework prop is not silently wrong for them.
func TestServerRoutes_OtherFrameworks(t *testing.T) {
	for _, tc := range []struct{ decl, want string }{
		{"const app = new Hono();", "hono"},
		{"const app = Fastify();", "fastify"},
		{"const app = fastify();", "fastify"},
		{"const app = new Koa();", "koa"},
	} {
		src := tc.decl + "\napp.get('/health/check', handler);\n"
		ff := extractTS(t, src, "src/server.ts")
		var fw string
		for _, f := range ff {
			if f.Kind == facts.KindRoute && f.Props["role"] == "server" {
				fw = f.Props["framework"].(string)
			}
		}
		if fw != tc.want {
			t.Errorf("%s: framework want %q, got %q", tc.decl, tc.want, fw)
		}
	}
}

// TestServerRoutes_TestFileEmitsNothing mirrors the v141/v142 gates: an e2e suite
// that spins up its own app is not a production surface.
func TestServerRoutes_TestFileEmitsNothing(t *testing.T) {
	src := `
const app = express();
app.get('/fixture/route', handler);
`
	for _, f := range []string{"server/index.e2e-spec.ts", "server/app.spec.ts", "e2e/server.e2e.ts"} {
		if got := serverRoutes(extractTS(t, src, f)); len(got) != 0 {
			t.Errorf("%s: %+v", f, got)
		}
	}
}

func TestServerRoutes_QuoteStyleDoesNotDecideExtraction(t *testing.T) {
	// Group indices for the path captures were off by one: a double-quoted path
	// read the single-quote group, a single-quoted path read the backtick group,
	// and a path that only populated the double-quote group walked past the
	// match layout — a PANIC on `api.get("/health")`, the most ordinary
	// registration there is. All three styles must extract identically.
	src := []byte(`import Fastify from 'fastify';
const api = Fastify();
api.get("/health", handler);
api.post('/jobs', handler);
api.put(` + "`/queue`" + `, handler);
`)
	ff := extractServerRouteFacts(src, "src/index.ts")
	want := map[string]bool{"/health": false, "/jobs": false, "/queue": false}
	for _, f := range ff {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("route %q missing — quote style must not decide extraction", path)
		}
	}
}

func TestServerRoutes_TypedFastifyInstanceParameter(t *testing.T) {
	src := `
import { FastifyInstance } from 'fastify';
import type { AppDeps } from './deps';

export function registerTrackRoute(app: FastifyInstance, deps: AppDeps): void {
  app.post('/v1/track', { bodyLimit: 1024 }, async (request, reply) => {
    return reply.send({});
  });
}
`
	ff := extractTS(t, src, "services/tracking-api/src/http/trackRoute.ts")
	got := serverRoutes(ff)
	if got["/v1/track"] != "POST" {
		t.Fatalf("typed FastifyInstance parameter must emit server POST /v1/track: %+v", got)
	}
	var fw, role string
	for _, f := range ff {
		if f.Kind == facts.KindRoute && f.Name == "/v1/track" {
			fw, _ = f.Props["framework"].(string)
			role, _ = f.Props["role"].(string)
		}
	}
	if fw != "fastify" || role != "server" {
		t.Fatalf("framework/role = %s/%s want fastify/server", fw, role)
	}
	if got := clientRoutes(ff); len(got) != 0 {
		t.Fatalf("must not also emit a client call: %+v", got)
	}
}

func TestServerRoutes_TypedFastifyAliasAndImportType(t *testing.T) {
	src := `
import type { FastifyInstance as App } from 'fastify';
export function register(server: App) {
  server.get('/health', async () => ({ ok: true }));
}
`
	ff := extractTS(t, src, "src/http/health.ts")
	if serverRoutes(ff)["/health"] != "GET" {
		t.Fatalf("aliased import type must bind: %+v", serverRoutes(ff))
	}
}

func TestServerRoutes_NameAppWithoutFastifyTypeStaysClient(t *testing.T) {
	src := `
export function register(app) {
  app.post('/v1/track', handler);
}
`
	ff := extractTS(t, src, "src/http/trackRoute.ts")
	if len(serverRoutes(ff)) != 0 {
		t.Fatalf("untyped app must not become a server route: %+v", serverRoutes(ff))
	}
	if clientRoutes(ff)["/v1/track"] != "POST" {
		t.Fatalf("untyped app.post must stay a client call: %+v", clientRoutes(ff))
	}
}

func TestServerRoutes_LocalFastifyInstanceTypeIsNotImported(t *testing.T) {
	src := `
type FastifyInstance = { post(path: string, h: unknown): void };
export function register(app: FastifyInstance) {
  app.post('/v1/track', handler);
}
`
	ff := extractTS(t, src, "src/http/trackRoute.ts")
	if len(serverRoutes(ff)) != 0 {
		t.Fatalf("local FastifyInstance type must not bind: %+v", serverRoutes(ff))
	}
}

func TestServerRoutes_DoesNotStealAxiosWhenTypedFastifyPresent(t *testing.T) {
	src := `
import { FastifyInstance } from 'fastify';
import axios from 'axios';
export function register(app: FastifyInstance) {
  app.get('/health', handler);
  axios.get('/external');
}
`
	ff := extractTS(t, src, "src/http/mix.ts")
	if serverRoutes(ff)["/health"] != "GET" {
		t.Fatalf("server route missing: %+v", serverRoutes(ff))
	}
	if clientRoutes(ff)["/external"] != "GET" {
		t.Fatalf("axios call must stay client: %+v", clientRoutes(ff))
	}
}

func TestServerRoutes_SiblingAxiosReceiverStaysClient(t *testing.T) {
	src := `
import type { FastifyInstance } from 'fastify';
import type { AxiosInstance } from 'axios';
export function register(app: FastifyInstance) { app.post('/server', async () => 'ok'); }
export async function send(app: AxiosInstance) { return app.post('/client', { x: 1 }); }
`
	ff := extractTS(t, src, "src/routes.ts")
	if serverRoutes(ff)["/server"] != "POST" {
		t.Fatalf("typed Fastify register must stay server: %+v", serverRoutes(ff))
	}
	if _, ok := serverRoutes(ff)["/client"]; ok {
		t.Fatalf("/client must not be a server route: %+v", serverRoutes(ff))
	}
	if clientRoutes(ff)["/client"] != "POST" {
		t.Fatalf("/client must stay axios client: %+v", clientRoutes(ff))
	}
}

func TestServerRoutes_FastifyPluginAsyncIsNotApp(t *testing.T) {
	src := `
import type { FastifyPluginAsync } from 'fastify';
export const plugin: FastifyPluginAsync = async (app) => {
  app.post('/maybe', handler);
};
`
	ff := extractTS(t, src, "src/plugin.ts")
	if len(serverRoutes(ff)) != 0 {
		t.Fatalf("plugin function type is not an app instance: %+v", serverRoutes(ff))
	}
}

func TestServerRoutes_CommentFastifyInstanceDoesNotBind(t *testing.T) {
	src := `
import type { AxiosInstance } from 'axios';
// import type { FastifyInstance } from 'fastify';
export async function send(app: AxiosInstance) { return app.post('/client', { x: 1 }); }
`
	ff := extractTS(t, src, "src/routes.ts")
	if len(serverRoutes(ff)) != 0 {
		t.Fatalf("commented FastifyInstance must not bind: %+v", serverRoutes(ff))
	}
	if clientRoutes(ff)["/client"] != "POST" {
		t.Fatalf("want client /client, got %+v", clientRoutes(ff))
	}
}

func TestServerRoutes_DoubleQuotedMountPrefix(t *testing.T) {
	src := []byte(`const express = require('express');
const app = express();
const router = express.Router();
app.use("/api", router);
router.get("/health", handler);
`)
	ff := extractServerRouteFacts(src, "server/index.js")
	found := false
	for _, f := range ff {
		if f.Name == "/api/health" {
			found = true
		}
	}
	if !found {
		t.Errorf("double-quoted mount prefix did not compose; got %+v", ff)
	}
}
