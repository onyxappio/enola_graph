package main

import (
	"context"

	sdk "github.com/enola-labs/enola/pkg/analyzerplugin"
)

func main() {
	registry := sdk.NewRegistry()
	if err := sdk.Register[sdk.PlanRequest, sdk.PlanResponse](registry, sdk.HookAnalysisPlan, func(context.Context, sdk.Host, sdk.PlanRequest) (sdk.PlanResponse, error) {
		return sdk.PlanResponse{Units: []sdk.UnitDecl{{ID: "fixture", Kind: "fixture"}}}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Register[sdk.AnalyzeUnitRequest, sdk.Contribution](registry, sdk.HookAnalysisUnit, func(context.Context, sdk.Host, sdk.AnalyzeUnitRequest) (sdk.Contribution, error) {
		return sdk.Contribution{Unit: "fixture", Owners: map[string]sdk.OwnerContribution{"src/machine.ts": {Nodes: []sdk.Node{
			{Kind: "fsm_state", Name: "product/state:generic", Owner: "src/machine.ts", Props: map[string]any{"state": "generic"}},
			{Kind: "extraction", Name: "product/coverage", Owner: "src/machine.ts", Props: map[string]any{"coverage": "generic"}},
		}}}}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Serve(registry); err != nil {
		panic(err)
	}
}
