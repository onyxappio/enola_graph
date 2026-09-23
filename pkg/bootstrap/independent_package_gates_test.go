package bootstrap

import "testing"

func TestIndependentPackageGateChangeIsLocal(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	graphWrite(t, root, "packages/database/package.json", `{"name":"database","dependencies":{"helper":"1","drizzle-orm":"1"}}`)
	apply("packages/database/package.json")
	graphWrite(t, root, "packages/clickhouse/package.json", `{"name":"clickhouse","devDependencies":{"helper":"1","drizzle-orm":"1"}}`)
	res := apply("packages/clickhouse/package.json")
	graphColdEqual(t, root, sink)
	if res.ParsedFiles != 1 {
		t.Fatalf("owning-package gate reparsed unrelated packages: parsed=%d invalidation=%+v", res.ParsedFiles, res.Invalidation)
	}
}

func TestIndependentRootGateRespectsNestedPackageShadow(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"react":"18","drizzle-orm":"1"}}`)
	res := apply("package.json")
	graphColdEqual(t, root, sink)
	if res.ParsedFiles != 3 {
		t.Fatalf("root gate escaped nested package shadow: parsed=%d invalidation=%+v", res.ParsedFiles, res.Invalidation)
	}
}
