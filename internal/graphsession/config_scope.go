package graphsession

import (
	"strings"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

// rawConfigScopeBounded decides whether a raw configuration byte change can be
// planned as an ordinary delta, or whether the frozen Begin has to replace every
// owner in the repository.
//
// The configuration fingerprint is one opaque digest, so it cannot say which
// byte moved. What it can say is what it covers, and every component of
// analysisFingerprintInputs has a projection this session already computes:
//
//   - the extractor version, proven by a complete prior state at this version;
//   - the graph input policy, whose options carry cfg.Ignore because that list
//     reaches graphinput.Options.Exclude, proven by PolicyAdmissionIdentity.
//     That fingerprint hashes the options and the semantic policy dependencies,
//     which is the whole of what the configuration contributes to the policy;
//     the tracked names the raw identity also hashes are repository state, not
//     configuration, and cannot be what a configuration edit moved;
//   - each ConfigKeyed extractor's config key, proven by EngineContextHash;
//   - the enabled extractor set, proven by the detected extractors together
//     with the extractors the stored state records having contributed;
//   - the bytes of every configuration path, proven by the TypeScript session
//     context. readRuntimeInputs hands SessionContext the same sorted path list
//     and the same captured bytes the fingerprint hashed, and that projection
//     either interprets a path - package.json validity, tsconfig alias parse,
//     selected root, framework and ORM gates, configured clients - or digests it
//     raw. An unprojected path is already a conservative byte comparison, so an
//     unchanged context is a statement about all of them, not only the known ones.
//
// Consumers other than TypeScript are bounded by their own declarations rather
// than here: a configuration path such an extractor reads is a declared content
// input or delta context, so an edit to it raises that extractor's need and
// seeds its owned files and retired owners into the frozen plan. That argument
// holds only for audited extractors, which is why an unaudited one declines even
// though ValidateGraphConsumers would already have failed the run.
//
// Declining returns the reason so the caller can record it as the fallback that
// produced a whole-domain Begin.
func (s *session) rawConfigScopeBounded(haveCache bool, input *runtimeInputs, detected map[string]bool, prevFiles map[string]*FileState) (bool, string) {
	if s.state == nil || !haveCache {
		return false, "no complete prior state to compare configuration projections against"
	}
	if s.eng.GraphScope() == nil {
		return false, "graph input policy inactive; no configuration projection is computed"
	}
	if s.state.EngineContextHash == "" || s.state.EngineContextHash != input.engineContextHash {
		return false, "engine extractor context changed with the configuration"
	}
	if s.state.PolicyAdmissionIdentity == "" || s.state.PolicyAdmissionIdentity != input.admissionIdentity {
		return false, "graph input policy admission identity changed with the configuration"
	}
	if len(s.state.TSContext) == 0 {
		return false, "no stored TypeScript session context to compare the configuration against"
	}
	if changes := tsextractor.ContextDifference(s.state.TSContext, input.tsContext); len(changes) > 0 {
		return false, "configuration moved the TypeScript session context: " + strings.Join(changes, ", ")
	}
	for _, ext := range s.eng.Extractors() {
		if detected[ext.Name()] && !engine.AuditedGraphConsumer(ext) {
			return false, "active extractor " + ext.Name() + " has unaudited configuration inputs"
		}
	}
	for _, name := range priorExtractorNames(s.state, prevFiles) {
		if detected[name] {
			continue
		}
		// The bounded seed walks the extractors this run detected, so one that
		// stopped being detected - which a changed cfg.Extractors list is one way
		// to cause - has no seeded owners to retire before Begin.
		return false, "extractor " + name + " contributed to the stored state and is no longer active"
	}
	return true, ""
}
