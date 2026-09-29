package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	sdk "github.com/enola-labs/enola/pkg/analyzerplugin"
)

type pluginConfig struct {
	Series string `json:"series"`
}

func main() {
	registry := sdk.NewRegistry()
	if err := sdk.Register[sdk.PlanRequest, sdk.PlanResponse](registry, sdk.HookAnalysisPlan, func(ctx context.Context, host sdk.Host, _ sdk.PlanRequest) (sdk.PlanResponse, error) {
		if _, err := sdk.DecodeConfig[pluginConfig](host); err != nil {
			return sdk.PlanResponse{}, err
		}
		paths, err := host.List(ctx, "**/*.md")
		if err != nil {
			return sdk.PlanResponse{}, err
		}
		units := make([]sdk.UnitDecl, 0, len(paths))
		for _, path := range paths {
			base := filepath.Base(path)
			switch base {
			case "summary-source.md":
				units = append(units, sdk.UnitDecl{ID: "summary:source", Kind: "summary-source", Params: map[string]any{"file": path}})
			case "summary-consumer.md":
				units = append(units, sdk.UnitDecl{ID: "summary:consumer", Kind: "summary-consumer", Params: map[string]any{"file": path}, Consumes: []string{"summary:source"}})
			default:
				units = append(units, sdk.UnitDecl{ID: "markdown:" + path, Kind: "markdown", Params: map[string]any{"file": path}})
			}
		}
		return sdk.PlanResponse{Units: units}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Register[sdk.AnalyzeUnitRequest, sdk.Contribution](registry, sdk.HookAnalysisUnit, func(ctx context.Context, host sdk.Host, request sdk.AnalyzeUnitRequest) (sdk.Contribution, error) {
		config, err := sdk.DecodeConfig[pluginConfig](host)
		if err != nil {
			return sdk.Contribution{}, err
		}
		path, _ := request.Unit.Params["file"].(string)
		doc, err := host.Read(ctx, path)
		if err != nil {
			return sdk.Contribution{}, err
		}
		if doc.Missing {
			return sdk.Contribution{}, fmt.Errorf("unit input %s disappeared", path)
		}
		if strings.Contains(doc.Text, "INVALID_OWNER") {
			return sdk.Contribution{Unit: request.Unit.ID, Owners: map[string]sdk.OwnerContribution{
				"outside/escape.md": {Nodes: []sdk.Node{{Kind: "task", Name: config.Series + "/invalid", Owner: "outside/escape.md"}}},
			}}, nil
		}
		if request.Unit.Kind == "summary-consumer" {
			raw, ok := request.Summaries["summary:source"]
			if !ok {
				return sdk.Contribution{}, fmt.Errorf("summary:source was not supplied")
			}
			var source string
			if err := json.Unmarshal(raw, &source); err != nil {
				return sdk.Contribution{}, fmt.Errorf("decode source summary: %w", err)
			}
			return sdk.Contribution{
				Unit: request.Unit.ID,
				Owners: map[string]sdk.OwnerContribution{path: {Nodes: []sdk.Node{{
					Kind: "task", Name: config.Series + "/summary-consumer", Owner: path,
					Props: map[string]any{"source_summary": source},
				}}}},
				Summary: doc.Text,
			}, nil
		}
		name := config.Series + "/T-1"
		if request.Unit.Kind == "summary-source" {
			name = config.Series + "/summary-source"
		}
		owner := sdk.OwnerContribution{Nodes: []sdk.Node{{
			Kind: "task", Name: name, Owner: path,
			Props:     map[string]any{"heading": doc.Text},
			Relations: []sdk.Relation{{Kind: "depends_on", Target: "sample/T-2", TargetKind: "task"}},
		}}, Enrichments: []sdk.Enrichment{{
			Kind: "task", Name: name, Owner: path,
			Props:     map[string]any{"status": "active"},
			Relations: []sdk.Relation{{Kind: "related", Target: "sample/T-2", TargetKind: "task"}},
		}}}
		return sdk.Contribution{Unit: request.Unit.ID, Owners: map[string]sdk.OwnerContribution{path: owner}, Summary: doc.Text}, nil
	}); err != nil {
		panic(err)
	}
	if err := sdk.Serve(registry); err != nil {
		panic(err)
	}
}
