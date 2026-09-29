# Generalized repository analyzer hooks

Status: implementation contract for the owner-directed T-001 generalization;
implementation and review are in progress. This amendment supersedes the
earlier V1 restriction that allowed only `enola.fsm@1`; it keeps
repository-owned code, explicit operator trust, observation-backed caching,
and frozen replacement scopes. The existing Node/FSM API remains a
compatibility adapter; the generalized authoring API is Go-first.

## What belongs in the repository

YAML registers the plugin and supplies opaque configuration. It does not
encode analyzer rules or enumerate a closed set of domain concepts. Since
Enola is a Go host, Go is the primary plugin authoring API; TypeScript/Node is
not a required toolchain:

```yaml
analyzer_plugins:
  - path: tools/enola-plugins/task-graph
    config:
      series: [enola-graph, product]
```

Its `enola-plugin.yaml` points to a prebuilt Go executable and declares the
hooks it registers:

```yaml
api: enola.plugin/v2
name: task-graph
runtime:
  kind: go-executable
  entry: dist/task-graph
hooks:
  - analysis.plan@1
  - analysis.unit@1
identity_files:
  - dist/task-graph
owner_domain: ["**/T-*.md"]
```

The repository plugin contains domain logic. A registry lets each plugin
implement only the hooks it supports; adding a method to the SDK does not force
an update to every existing plugin:

```go
// Skeleton; parseTaskFrontmatter is repository-specific domain code.
package main

import (
    "context"
    "errors"

    sdk "github.com/enola-labs/enola/pkg/analyzerplugin"
)

func registerHooks(r *sdk.Registry) error {
    if err := sdk.Register[sdk.PlanRequest, sdk.PlanResponse](
        r, sdk.HookAnalysisPlan, plan,
    ); err != nil {
        return err
    }
    return sdk.Register[sdk.AnalyzeUnitRequest, sdk.Contribution](
        r, sdk.HookAnalysisUnit, analyzeUnit,
    )
}

func plan(ctx context.Context, host sdk.Host, _ sdk.PlanRequest) (sdk.PlanResponse, error) {
    files, err := host.List(ctx, "**/T-*.md")
    if err != nil {
        return sdk.PlanResponse{}, err
    }
    units := make([]sdk.UnitDecl, 0, len(files))
    for _, file := range files {
        units = append(units, sdk.UnitDecl{
            ID: "markdown:" + file, Kind: "markdown",
            Params: map[string]any{"file": file},
        })
    }
    return sdk.PlanResponse{Units: units}, nil
}

func analyzeUnit(ctx context.Context, host sdk.Host, req sdk.AnalyzeUnitRequest) (sdk.Contribution, error) {
    file, ok := req.Unit.Params["file"].(string)
    if !ok {
        return sdk.Contribution{}, errors.New("unit is missing its file parameter")
    }
    doc, err := host.Read(ctx, file)
    if err != nil {
        return sdk.Contribution{}, err
    }
    task, err := parseTaskFrontmatter(doc.Text)
    if err != nil {
        return sdk.Contribution{}, err
    }
    name := task.Series + "/" + task.ID
    relations := make([]sdk.Relation, 0, len(task.Dependencies))
    for _, dependency := range task.Dependencies {
        relations = append(relations, sdk.Relation{
            Kind: "depends_on", Target: dependency, TargetKind: "task",
        })
    }
    return sdk.Contribution{
        Unit: req.Unit.ID,
        Owners: map[string]sdk.OwnerContribution{file: {
            Nodes: []sdk.Node{{
                Kind: "task", Name: name, Owner: file,
                Props: map[string]any{"status": task.Status},
                Relations: relations,
            }},
            Enrichments: []sdk.Enrichment{{
                Kind: "task", Name: name, Owner: file,
                Props: map[string]any{"title": task.Title},
            }},
        }},
    }, nil
}
```

This is the target authoring API. The `enola.plugin/v2` implementation runs a
repository-built Go executable behind the process boundary. The plugin
manifest points at the built executable; source repositories build it for
their target OS/architecture. The Go SDK hides transport details and provides
a registry for typed handlers, so plugin authors write hook logic rather than
an NDJSON loop. The initial versioned hooks are `analysis.plan@1` and
`analysis.unit@1`; adding another hook uses the same registry and framing
protocol. This avoids Go's standard
`plugin` shared-library loader, which ties host and plugin to matching builds
and is unsupported on every platform. The executable starts only when its plan
or units need work; unchanged analysis starts no plugin process. Existing V1
Node/FSM registrations continue through the compatibility adapter. The
implementation remains under review; this document does not claim T-001 has
passed its full acceptance or delivery gates.

Hook calls use stable names and versions with typed request/response payloads.
The registry advertises supported hook capabilities during `hello`; required
capabilities must match, while optional unknown capabilities do not prevent an
older plugin from running. `plan` and `analyze_unit` are initial hook IDs.
Adding another hook adds its host invocation point, request/response types and
SDK registration, without changing the framing protocol or requiring existing
plugins to implement it. A single generic SDK `RegisterHook`/dispatcher path
should serve all hook IDs rather than adding a new transport operation per
hook.

## Generic graph contributions

Plugin code may create plugin-owned node kinds, add relations, attach
plugin-namespaced properties and enrich an existing fact through a stable
owner/kind/name target. The host applies contributions as owner-scoped
replacements; a plugin never mutates the committed graph directly. Plugin node
and relation kinds are namespaced by plugin identity to prevent collisions.
Relations carry their target kind so graph lookup can resolve generic typed
edges without relying on an FSM-only global relation table. Properties are
namespaced per plugin and cannot overwrite host-owned fields.

Domain meaning stays in plugin code. Enola validates the generic envelope:
plugin namespace, stable identities, source spans, owner domain, relation
shape, target-kind consistency, deterministic ordering, and duplicate
contributions. The built-in FSM vocabulary remains supported through an
optional compatibility adapter; FSM names and machine-claim rules do not
constrain other plugins.

## Hook and invalidation contract

- The host invokes versioned `plan` and `analyze_unit` hooks before replacement
  begins. `plan` discovers stable work units and declares producer/consumer
  summary dependencies.
- `analyze_unit` runs for invalid units and returns the complete current
  contribution for each owner it writes.
- Optional graph enrichments are returned as contributions addressed by
  stable fact identity and owner; they are merged after the host has the base
  and plugin-created facts available. Missing or ambiguous targets fail the
  run, and plugin properties are namespaced to prevent host-field overwrite.
- Repository reads, probes, listings and resolution requests go through the
  host API so every answer is recorded as an invalidation observation. A
  graph relation such as `depends_on` is semantic output; it is distinct from
  the host's file/unit dependency observations.
- The host freezes the complete owner replacement scope before Begin. An
  out-of-scope write fails the run without EndReplace or generation advance.
  Unchanged inputs still require zero plugin subprocesses and zero events.

## Scope and proof

T-001 supplies a Go non-FSM fixture that creates its own fact kinds,
properties and typed relations through the hooks, plus compatibility coverage
for the existing FSM contributions. This proves the host contract can represent
both FSM facts and a different domain such as tasks-hub's `Task`/`Feedback`
entities and `depends_on`/`related`/`spawned_by` relations. Stable task identity
must be independent of status-folder paths; owner replacement handles a move
from the old Markdown path to the new one. T-001 does not implement the
tasks-hub frontmatter adapter. A separate integration follow-up remains
necessary because Codata's `graph-command` does not yet pass
`--allow-repo-plugins`, and `ts-enola` discovery is currently TypeScript
oriented.

The implementation must preserve cold/delta equality, fail-closed behavior,
owner-scoped replacement, lockfile exclusions, zero-work no-change behavior,
and the existing performance acceptance criteria. No numeric overhead target
is added by this amendment.
