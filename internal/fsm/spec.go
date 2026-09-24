// Package fsm defines the repository-configured finite-state-machine profile.
// Configuration admits concrete machine declarations; syntax and spelling by
// themselves never admit a machine.
package fsm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	AdapterRuleTable          = "rule_table"
	AdapterReducerInterpreter = "reducer_interpreter"
)

// SymbolRef identifies an actual declaration by repository-relative source
// module and exported name. The extractor resolves this reference through
// imports and re-exports before treating its use as machine evidence.
type SymbolRef struct {
	Module string `yaml:"module" json:"module"`
	Export string `yaml:"export" json:"export"`
}

// DispatchSink describes the declaration of a configured runtime sink. A
// reducer-backed runtime commonly returns an object with a send method; the
// configured source declaration is followed through the caller's bindings.
type DispatchSink struct {
	Factory SymbolRef `yaml:"factory" json:"factory"`
	Method  string    `yaml:"method" json:"method"`
	// Function-sink argument binding is used with MachineTag. The path is a
	// dot-separated, same-function value path rooted at EventArgument (for
	// example argument 2, path "event"). It is intentionally not a general
	// data-flow language.
	MachineArgument int    `yaml:"machine_argument,omitempty" json:"machine_argument,omitempty"`
	EventArgument   int    `yaml:"event_argument,omitempty" json:"event_argument,omitempty"`
	EventPath       string `yaml:"event_path,omitempty" json:"event_path,omitempty"`
	// MachineTag is used by Effect service patterns where a sink receives the
	// machine through a resolved Context.Tag and a provided Layer.
	MachineTag *SymbolRef `yaml:"machine_tag,omitempty" json:"machine_tag,omitempty"`
}

// Spec opts a repository into one known family and identifies its semantic
// declarations. Product-specific names belong in that repository's Enola
// configuration, not in the defaults shipped by Enola.
type Spec struct {
	ID                  string         `yaml:"id" json:"id"`
	Adapter             string         `yaml:"adapter" json:"adapter"`
	File                string         `yaml:"file" json:"file"`
	Factory             SymbolRef      `yaml:"factory,omitempty" json:"factory,omitempty"`
	InstanceExport      string         `yaml:"instance_export,omitempty" json:"instance_export,omitempty"`
	Registration        string         `yaml:"registration,omitempty" json:"registration,omitempty"`
	StateType           string         `yaml:"state_type,omitempty" json:"state_type,omitempty"`
	EventType           string         `yaml:"event_type,omitempty" json:"event_type,omitempty"`
	CommandType         string         `yaml:"command_type,omitempty" json:"command_type,omitempty"`
	EffectType          string         `yaml:"effect_type,omitempty" json:"effect_type,omitempty"`
	CommandDiscriminant string         `yaml:"command_discriminant,omitempty" json:"command_discriminant,omitempty"`
	Dispatcher          string         `yaml:"dispatcher,omitempty" json:"dispatcher,omitempty"`
	Start               string         `yaml:"start,omitempty" json:"start,omitempty"`
	Enter               string         `yaml:"enter,omitempty" json:"enter,omitempty"`
	Stay                string         `yaml:"stay,omitempty" json:"stay,omitempty"`
	Reject              string         `yaml:"reject,omitempty" json:"reject,omitempty"`
	Entry               string         `yaml:"entry,omitempty" json:"entry,omitempty"`
	Settlements         []string       `yaml:"settlements,omitempty" json:"settlements,omitempty"`
	EffectRunner        *SymbolRef     `yaml:"effect_runner,omitempty" json:"effect_runner,omitempty"`
	HandlerFiles        []string       `yaml:"handler_files,omitempty" json:"handler_files,omitempty"`
	DispatchFiles       []string       `yaml:"dispatch_files,omitempty" json:"dispatch_files,omitempty"`
	EventParameter      string         `yaml:"event_parameter,omitempty" json:"event_parameter,omitempty"`
	SnapshotParameter   string         `yaml:"snapshot_parameter,omitempty" json:"snapshot_parameter,omitempty"`
	DispatchSinks       []DispatchSink `yaml:"dispatch_sinks,omitempty" json:"dispatch_sinks,omitempty"`
}

// Consumer is implemented by extractors that execute the configured FSM profile.
type Consumer interface {
	SetStateMachineSpecs([]Spec)
}

func NormalizeAndValidate(specs []Spec) error {
	seen := map[string]bool{}
	for i := range specs {
		s := &specs[i]
		s.ID = strings.TrimSpace(s.ID)
		s.Adapter = strings.TrimSpace(s.Adapter)
		s.File = cleanRepoPath(s.File)
		if s.ID == "" || strings.ContainsAny(s.ID, "\x00\n\r") {
			return fmt.Errorf("state_machines[%d]: missing or invalid id", i)
		}
		if seen[s.ID] {
			return fmt.Errorf("state_machines[%d]: duplicate id %q", i, s.ID)
		}
		seen[s.ID] = true
		if !validRepoFile(s.File) {
			return fmt.Errorf("state_machines[%d]: file must be a repository-relative path", i)
		}
		s.Registration = strings.TrimSpace(s.Registration)
		s.InstanceExport = strings.TrimSpace(s.InstanceExport)
		s.StateType = strings.TrimSpace(s.StateType)
		s.EventType = strings.TrimSpace(s.EventType)
		s.CommandType = strings.TrimSpace(s.CommandType)
		s.EffectType = strings.TrimSpace(s.EffectType)
		s.Dispatcher = strings.TrimSpace(s.Dispatcher)
		s.Start = strings.TrimSpace(s.Start)
		s.Enter = strings.TrimSpace(s.Enter)
		s.Stay = strings.TrimSpace(s.Stay)
		s.Reject = strings.TrimSpace(s.Reject)
		s.Entry = strings.TrimSpace(s.Entry)
		switch s.Adapter {
		case AdapterRuleTable:
			if err := normalizeSymbolRef(&s.Factory); err != nil {
				return fmt.Errorf("state_machines[%d].factory: %w", i, err)
			}
			if s.Factory.Module == "" || s.Factory.Export == "" {
				return fmt.Errorf("state_machines[%d]: rule_table requires factory.module and factory.export", i)
			}
			if s.Registration == "" {
				return fmt.Errorf("state_machines[%d]: rule_table requires registration", i)
			}
		case AdapterReducerInterpreter:
			if s.Dispatcher == "" || s.Enter == "" || s.StateType == "" || s.EventType == "" {
				return fmt.Errorf("state_machines[%d]: reducer_interpreter requires dispatcher, enter, state_type, and event_type", i)
			}
		default:
			return fmt.Errorf("state_machines[%d]: unsupported adapter %q", i, s.Adapter)
		}
		if s.EffectRunner != nil {
			if err := normalizeSymbolRef(s.EffectRunner); err != nil {
				return fmt.Errorf("state_machines[%d].effect_runner: %w", i, err)
			}
		}
		for j := range s.HandlerFiles {
			s.HandlerFiles[j] = cleanRepoPath(s.HandlerFiles[j])
			if !validRepoFile(s.HandlerFiles[j]) {
				return fmt.Errorf("state_machines[%d].handler_files[%d]: expected repository-relative path", i, j)
			}
		}
		for j := range s.DispatchFiles {
			s.DispatchFiles[j] = cleanRepoPath(s.DispatchFiles[j])
			if !validRepoFile(s.DispatchFiles[j]) {
				return fmt.Errorf("state_machines[%d].dispatch_files[%d]: expected repository-relative path", i, j)
			}
		}
		for j := range s.DispatchSinks {
			d := &s.DispatchSinks[j]
			if err := normalizeSymbolRef(&d.Factory); err != nil {
				return fmt.Errorf("state_machines[%d].dispatch_sinks[%d].factory: %w", i, j, err)
			}
			d.Method = strings.TrimSpace(d.Method)
			if d.Factory.Module == "" || d.Factory.Export == "" || d.Method == "" {
				return fmt.Errorf("state_machines[%d].dispatch_sinks[%d]: factory.module, factory.export, and method are required", i, j)
			}
			if d.MachineTag != nil {
				if d.MachineArgument < 0 || d.EventArgument < 0 {
					return fmt.Errorf("state_machines[%d].dispatch_sinks[%d]: argument indexes must be non-negative", i, j)
				}
				if err := normalizeSymbolRef(d.MachineTag); err != nil {
					return fmt.Errorf("state_machines[%d].dispatch_sinks[%d].machine_tag: %w", i, j, err)
				}
				if d.MachineTag.Module == "" || d.MachineTag.Export == "" {
					return fmt.Errorf("state_machines[%d].dispatch_sinks[%d]: machine_tag requires module and export", i, j)
				}
			}
			if d.EventPath != "" {
				d.EventPath = strings.TrimSpace(d.EventPath)
				for _, part := range strings.Split(d.EventPath, ".") {
					if part == "" || part == ".." {
						return fmt.Errorf("state_machines[%d].dispatch_sinks[%d]: event_path must contain property names relative to event_argument", i, j)
					}
				}
			}
		}
		if len(s.DispatchSinks) > 0 && len(s.DispatchFiles) == 0 {
			return fmt.Errorf("state_machines[%d]: dispatch_sinks require explicit dispatch_files for coverage accounting", i)
		}
		s.CommandDiscriminant = strings.TrimSpace(s.CommandDiscriminant)
		s.HandlerFiles = uniqueSorted(s.HandlerFiles)
		s.DispatchFiles = uniqueSorted(s.DispatchFiles)
		s.Settlements = uniqueSorted(s.Settlements)
		sort.Slice(s.DispatchSinks, func(i, j int) bool {
			return sinkKey(s.DispatchSinks[i]) < sinkKey(s.DispatchSinks[j])
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return nil
}

func Fingerprint(specs []Spec) string {
	if len(specs) == 0 {
		return ""
	}
	copySpecs := cloneSpecs(specs)
	if err := NormalizeAndValidate(copySpecs); err != nil {
		// Invalid configuration is still identity-bearing. Config.Normalize
		// reports the error before extraction; this branch avoids key collisions
		// for a directly constructed extractor used by a caller.
		return "invalid:" + err.Error()
	}
	b, _ := json.Marshal(copySpecs)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func cloneSpecs(specs []Spec) []Spec {
	out := append([]Spec(nil), specs...)
	for i := range out {
		out[i].HandlerFiles = append([]string(nil), specs[i].HandlerFiles...)
		out[i].DispatchFiles = append([]string(nil), specs[i].DispatchFiles...)
		out[i].Settlements = append([]string(nil), specs[i].Settlements...)
		out[i].DispatchSinks = append([]DispatchSink(nil), specs[i].DispatchSinks...)
		if specs[i].EffectRunner != nil {
			ref := *specs[i].EffectRunner
			out[i].EffectRunner = &ref
		}
		for j := range out[i].DispatchSinks {
			if specs[i].DispatchSinks[j].MachineTag != nil {
				ref := *specs[i].DispatchSinks[j].MachineTag
				out[i].DispatchSinks[j].MachineTag = &ref
			}
		}
	}
	return out
}

func normalizeSymbolRef(ref *SymbolRef) error {
	if ref == nil {
		return fmt.Errorf("reference is required")
	}
	ref.Module = cleanRepoPath(ref.Module)
	ref.Export = strings.TrimSpace(ref.Export)
	if ref.Module != "" && !validRepoFile(ref.Module) {
		return fmt.Errorf("module must be a repository-relative file path")
	}
	if strings.ContainsAny(ref.Export, "\x00\n\r") {
		return fmt.Errorf("export is invalid")
	}
	return nil
}

func validRepoFile(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !path.IsAbs(value)
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if value == "" || (len(out) > 0 && out[len(out)-1] == value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func sinkKey(sink DispatchSink) string {
	b, _ := json.Marshal(sink)
	return string(b)
}

func cleanRepoPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean(value), "./")
}
