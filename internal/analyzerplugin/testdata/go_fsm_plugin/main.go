package main

import (
	"context"
	"strings"

	sdk "github.com/enola-labs/enola/pkg/analyzerplugin"
)

func main() {
	registry := sdk.NewRegistry()
	if err := sdk.Register[sdk.PlanRequest, sdk.PlanResponse](registry, sdk.HookAnalysisPlan, func(context.Context, sdk.Host, sdk.PlanRequest) (sdk.PlanResponse, error) {
		return sdk.PlanResponse{Units: []sdk.UnitDecl{
			{ID: "census:extra", Kind: "census"},
			{ID: "model:product", Kind: "model", Params: map[string]any{"file": "src/machine.ts"}},
		}}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Register[sdk.AnalyzeUnitRequest, sdk.Contribution](registry, sdk.HookAnalysisUnit, func(ctx context.Context, host sdk.Host, request sdk.AnalyzeUnitRequest) (sdk.Contribution, error) {
		if request.Unit.ID == "census:extra" {
			return sdk.Contribution{Unit: request.Unit.ID, Owners: map[string]sdk.OwnerContribution{}, Census: &sdk.Census{ParsedFiles: 2}}, nil
		}
		doc, err := host.Read(ctx, "src/machine.ts")
		if err != nil {
			return sdk.Contribution{}, err
		}
		nestedDoc, nestedOwner := doc, "src/machine.ts"
		if app, err := host.Read(ctx, "src/app.ts"); err == nil && strings.Contains(app.Text, "NESTED_PARITY_FIXTURE") {
			nestedDoc, nestedOwner = app, "src/app.ts"
		} else if app, err := host.Read(ctx, "src/App.tsx"); err == nil && strings.Contains(app.Text, "NESTED_PARITY_FIXTURE") {
			nestedDoc, nestedOwner = app, "src/App.tsx"
		}
		nodes := []sdk.Node{
			{Kind: "fsm_machine", Name: "product", Owner: "src/machine.ts", Props: map[string]any{"adapter": "fixture"}, Relations: []sdk.Relation{{Kind: "declares", Target: "src", TargetKind: "module"}, {Kind: "fsm_initial", Target: "product/state:idle", TargetKind: "fsm_state"}}},
			{Kind: "fsm_state", Name: "product/state:idle", Owner: "src/machine.ts", Props: map[string]any{"state": "idle"}, Relations: []sdk.Relation{{Kind: "declares", Target: "product"}}},
			{Kind: "fsm_event", Name: "product/event:start", Owner: "src/machine.ts", Relations: []sdk.Relation{{Kind: "declares", Target: "product"}}},
			{Kind: "fsm_command", Name: "product/command:start", Owner: "src/machine.ts", Relations: []sdk.Relation{{Kind: "declares", Target: "product"}}},
			{Kind: "fsm_transition", Name: "product/transition:idle-start", Owner: "src/machine.ts", Relations: []sdk.Relation{{Kind: "fsm_from", Target: "product/state:idle", TargetKind: "fsm_state"}, {Kind: "fsm_to", Target: "product/state:idle", TargetKind: "fsm_state"}, {Kind: "fsm_on", Target: "product/event:start", TargetKind: "fsm_event"}, {Kind: "fsm_emits", Target: "product/command:start", TargetKind: "fsm_command"}}},
			{Kind: "extraction", Name: "plugin:product-fsm:coverage", Owner: "src/machine.ts", Props: map[string]any{"edge_coverage": "partial"}},
			{Kind: "diagnostic", Name: "product/diagnostic", Owner: "src/machine.ts", Props: map[string]any{"detail": "generic"}, Relations: []sdk.Relation{{Kind: "mentions", Target: "product", TargetKind: "fsm_machine"}}},
		}
		flatLine := 1
		if at := strings.Index(doc.Text, "function dispatch"); at >= 0 {
			flatLine += strings.Count(doc.Text[:at], "\n")
		}
		anchors := []sdk.Anchor{{Symbol: "dispatch", Line: flatLine, Props: map[string]any{"anchor_detail": "flat"}, Relations: []sdk.Relation{{Kind: "fsm_dispatches", Target: "product/event:start", TargetKind: "fsm_event"}}}}
		if nestedOwner != "src/machine.ts" {
			// The cross-owner nested parity fixture has no flat source anchor.
			anchors = nil
		}
		var nestedNodes []sdk.Node
		var nestedAnchors []sdk.Anchor
		if !strings.Contains(nestedDoc.Text, "NO_NESTED_HELPER") {
			helperLine := 1
			helperSymbol := "dispatch"
			if at := strings.Index(nestedDoc.Text, "dispatch() {"); at >= 0 {
				helperLine += strings.Count(nestedDoc.Text[:at], "\n")
			} else if at := strings.Index(nestedDoc.Text, "const dispatchIntent"); at >= 0 {
				helperLine += strings.Count(nestedDoc.Text[:at], "\n")
				helperSymbol = "dispatchIntent"
			} else if at := strings.Index(nestedDoc.Text, "const dispatch"); at >= 0 {
				helperLine += strings.Count(nestedDoc.Text[:at], "\n")
			}
			helperName := "src.Container.dispatch"
			if strings.Contains(nestedDoc.Text, "NESTED_PARITY_FIXTURE") && strings.Contains(nestedDoc.Text, "function App") {
				helperName = "src.App." + helperSymbol
			}
			helperNode := sdk.Node{Kind: "symbol", Name: helperName, Owner: nestedOwner, Line: helperLine, EndLine: helperLine, Props: map[string]any{"language": "typescript", "symbol_kind": "function", "fsm_source_binding": "tree_sitter_nested_declaration"}}
			if nestedOwner == "src/machine.ts" {
				nodes = append(nodes, helperNode)
			} else {
				nestedNodes = append(nestedNodes, helperNode)
			}
			nested := sdk.Anchor{Symbol: helperSymbol, Owner: nestedOwner, Line: helperLine, EndLine: helperLine, SourceIdentity: helperName, Relations: []sdk.Relation{{Kind: "fsm_dispatches", Target: "product/event:start", TargetKind: "fsm_event"}}}
			if nestedOwner == "src/machine.ts" {
				anchors = append(anchors, nested)
			} else {
				nestedAnchors = append(nestedAnchors, nested)
			}
		}
		owners := map[string]sdk.OwnerContribution{"src/machine.ts": {Nodes: nodes, Anchors: anchors}}
		if len(nestedAnchors) > 0 {
			owners[nestedOwner] = sdk.OwnerContribution{Nodes: nestedNodes, Anchors: nestedAnchors}
		}
		return sdk.Contribution{
			Unit:   request.Unit.ID,
			Owners: owners,
			Census: &sdk.Census{ParsedFiles: 3},
		}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Serve(registry); err != nil {
		panic(err)
	}
}
