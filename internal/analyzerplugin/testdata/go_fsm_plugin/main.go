package main

import (
	"context"

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
		if _, err := host.Read(ctx, "src/machine.ts"); err != nil {
			return sdk.Contribution{}, err
		}
		return sdk.Contribution{
			Unit: request.Unit.ID,
			Owners: map[string]sdk.OwnerContribution{"src/machine.ts": {
				Nodes: []sdk.Node{
					{Kind: "fsm_machine", Name: "product", Owner: "src/machine.ts", Props: map[string]any{"adapter": "fixture"}},
					{Kind: "fsm_state", Name: "product/state:idle", Owner: "src/machine.ts", Props: map[string]any{"state": "idle"}, Relations: []sdk.Relation{{Kind: "fsm_parent", Target: "product/state:idle", TargetKind: "fsm_state"}}},
					{Kind: "fsm_event", Name: "product/event:start", Owner: "src/machine.ts"},
					{Kind: "fsm_command", Name: "product/command:start", Owner: "src/machine.ts"},
					{Kind: "fsm_transition", Name: "product/transition:idle-start", Owner: "src/machine.ts", Relations: []sdk.Relation{{Kind: "fsm_from", Target: "product/state:idle", TargetKind: "fsm_state"}, {Kind: "fsm_to", Target: "product/state:idle", TargetKind: "fsm_state"}, {Kind: "fsm_on", Target: "product/event:start", TargetKind: "fsm_event"}}},
					{Kind: "extraction", Name: "plugin:product-fsm:coverage", Owner: "src/machine.ts", Props: map[string]any{"edge_coverage": "partial"}},
					{Kind: "diagnostic", Name: "product/diagnostic", Owner: "src/machine.ts", Props: map[string]any{"detail": "generic"}, Relations: []sdk.Relation{{Kind: "mentions", Target: "product", TargetKind: "fsm_machine"}}},
				},
				Anchors: []sdk.Anchor{{Symbol: "dispatch", Line: 1, Props: map[string]any{"anchor_detail": "flat"}, Relations: []sdk.Relation{{Kind: "fsm_dispatches", Target: "product/event:start", TargetKind: "fsm_event"}}}},
			}},
			Census: &sdk.Census{ParsedFiles: 3},
		}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Serve(registry); err != nil {
		panic(err)
	}
}
