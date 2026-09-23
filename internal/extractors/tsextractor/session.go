package tsextractor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"

	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/enola-labs/enola/internal/extractors/extcoverage"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/parallel"
)

// ExtractStats counts real work. CachedFiles are reused without reading source.
type ExtractStats struct {
	FilesRead     int `json:"files_read"`
	FilesParsed   int `json:"files_parsed"`
	CachedFiles   int `json:"cached_files"`
	GraphQLParsed int `json:"graphql_parsed"`
	SFCParsed     int `json:"sfc_parsed"`
	SummaryScans  int `json:"summary_scans"`
}

// FileRecord is the immutable per-file contribution persisted between generations.
type FileRecord struct {
	File       string       `json:"file"`
	Hash       string       `json:"hash,omitempty"`
	Unreadable bool         `json:"unreadable,omitempty"`
	Minified   bool         `json:"minified,omitempty"`
	Facts      []facts.Fact `json:"facts,omitempty"`
	// ImportSpecs are alias/relative-normalized replay paths (resolveImportPath
	// output), not the exact bound file. Membership replay compares these
	// against old/new filename universes; ResolvedFiles hold exact provenance.
	ImportSpecs     []string `json:"import_specs,omitempty"`
	ResolvedFiles   []string `json:"resolved_files,omitempty"`
	Declared        []string `json:"declared,omitempty"`
	Referenced      []string `json:"referenced,omitempty"`
	Reexports       []string `json:"reexports,omitempty"`
	UnresolvedSpecs []string `json:"unresolved_specs,omitempty"`
	// ImportComplete is set after summarizeFacts runs. Empty resolved and
	// unresolved lists are a valid graph when every import is external.
	ImportComplete bool `json:"import_complete,omitempty"`
	// SideReads are other source files whose bytes were consulted to derive
	// this file's facts (named re-export chains). Hash changes there invalidate
	// this contribution even when this file is untouched.
	SideReads      []string          `json:"side_reads,omitempty"`
	SideReadHashes map[string]string `json:"side_read_hashes,omitempty"`
	// ExportSurface is the file's observable export membership as the binder sees
	// it - the encoded namedExportIndex, see (*namedExportIndex).surface. Nothing
	// else on this record describes it: Declared holds every declaration whether
	// exported or not, so dropping `export` from one of them, or removing a name
	// from an `export { ... }` clause, moves no other field while consumers of
	// that name stop resolving. ExportSurfaceRecorded says the surface was
	// computed rather than merely empty, so a record written before this field
	// existed reads as unknown instead of as a file that exports nothing.
	ExportSurface         []string    `json:"export_surface,omitempty"`
	ExportSurfaceRecorded bool        `json:"export_surface_recorded,omitempty"`
	GraphQLServer         bool        `json:"graphql_server,omitempty"`
	GraphQLSDL            []string    `json:"graphql_sdl,omitempty"`
	GraphQLParsed         bool        `json:"graphql_parsed,omitempty"`
	GRPC                  *GRPCRecord `json:"grpc,omitempty"`
	Router                *RouterDTO  `json:"router,omitempty"`
	ParseKind             string      `json:"parse_kind,omitempty"`
	AutoImportDirs        []string    `json:"auto_import_dirs,omitempty"`
}

// BindsNoImports reports whether the file resolves no import or re-export of its
// own, and so cannot be a link in anyone's re-export chain. It is both the
// condition under which an export surface is worth recording and the condition
// under which one may be trusted, kept in one place so those two cannot drift
// apart: a file the extractor judged ineligible must never be one the planner
// judged provable.
func (r *FileRecord) BindsNoImports() bool {
	return r != nil && len(r.ImportSpecs) == 0 && len(r.ResolvedFiles) == 0 &&
		len(r.UnresolvedSpecs) == 0 && len(r.Reexports) == 0
}

// SessionResult is one TS analysis pass under the local-fact contract.
type SessionResult struct {
	Facts       []facts.Fact
	Records     map[string]*FileRecord
	Stats       ExtractStats
	ConfigPaths []string
	Angular     bool
	Unreadable  []string
}

// SessionHooks observe per-file local results before repo-wide composition.
type SessionHooks struct {
	// SkipConfigPaths omits the informational result inventory when the caller
	// independently discovers and revalidates configuration inputs for the run.
	// It does not alter extraction, config reads, or source overlays.
	SkipConfigPaths bool
	OnFileLocal     func(rec *FileRecord)
	// OnBeforeParse is invoked on each dirty file immediately before extractFile.
	// Tests use it to block later-file analysis while an earlier local batch is in flight.
	OnBeforeParse func(rel string)
	// Sources, when set, are the exact bytes extraction must consume for those
	// relative paths (source and config). Missing keys are read from disk.
	Sources map[string][]byte
}

// ExtractSession runs the TypeScript extractor with optional cached file
// contributions. dirty==nil means every owned file is reparsed. Unchanged
// records are reused as-is; their source is not read.
func (e *TSExtractor) ExtractSession(ctx context.Context, repoPath string, files []string, prev map[string]*FileRecord, dirty map[string]bool, hooks SessionHooks) (*SessionResult, error) {
	inputScope := e.inputScope
	if prev == nil {
		prev = map[string]*FileRecord{}
	}
	ctx = withFileOverlay(ctx, newFileOverlay(repoPath, hooks.Sources))
	allDirty := dirty == nil
	tr := graphprofile.Start()

	isNextJS := detectNextJS(repoPath, inputScope)
	nuxtPkgs := collectNuxtPackages(ctx, repoPath, inputScope)
	isNuxt := len(nuxtPkgs) > 0
	isSvelteKit := detectSvelteKit(repoPath, inputScope)
	isEmber := detectEmber(repoPath, inputScope)
	isReactNav := detectReactNavigation(repoPath, inputScope)
	isAngular := detectAngular(repoPath, inputScope)
	pkgGates := collectPackageGates(ctx, repoPath, inputScope)
	pkgNamesEarly := collectPackageNames(repoPath, inputScope)
	isPrisma := pkgGates.anyPrisma
	aliasRoots := collectTSAliasRoots(ctx, repoPath, inputScope)
	if isSvelteKit {
		aliasRoots = withSvelteKitAliasFallbacks(repoPath, aliasRoots, inputScope)
	}
	if isNuxt {
		aliasRoots = withNuxtAliasFallbacks(repoPath, aliasRoots, nuxtPkgs, inputScope)
	}
	tr.Mark("ts_detect_frameworks", fmt.Sprintf("files=%d", len(files)))

	var tsFiles, htmlFiles []string
	knownFiles := make(map[string]bool)
	for _, relFile := range files {
		if isTypeScriptFile(relFile) {
			tsFiles = append(tsFiles, relFile)
			knownFiles[filepath.ToSlash(relFile)] = true
			continue
		}
		if isAngular && isAngularTemplateFile(relFile) && !facts.IsTestPath(relFile) {
			htmlFiles = append(htmlFiles, relFile)
		}
	}
	pkgAliases := collectPackageAliases(ctx, repoPath, knownFiles, inputScope)

	need := func(rel string) bool {
		if allDirty {
			return true
		}
		if dirty[rel] {
			return true
		}
		rec := prev[rel]
		if rec == nil {
			return true
		}
		for _, f := range rec.ResolvedFiles {
			if !knownFiles[filepath.ToSlash(f)] {
				return true
			}
		}
		for _, spec := range rec.UnresolvedSpecs {
			if _, ok := NormalizeImportTarget(spec, knownFiles); ok {
				return true
			}
		}
		for _, f := range rec.SideReads {
			if !knownFiles[filepath.ToSlash(f)] {
				return true
			}
		}
		return false
	}

	var stats ExtractStats
	sources := make(map[string][]byte, len(tsFiles))
	toRead := make([]string, 0, len(tsFiles))
	for _, rel := range tsFiles {
		if need(rel) {
			toRead = append(toRead, rel)
		} else {
			stats.CachedFiles++
		}
	}
	readSources := parallel.MapFiles(ctx, toRead, func(relFile string) struct {
		src []byte
		err error
	} {
		if hooks.Sources != nil {
			if src, ok := hooks.Sources[relFile]; ok {
				return struct {
					src []byte
					err error
				}{src, nil}
			}
		}
		src, err := overlayReadFile(ctx, filepath.Join(repoPath, relFile), inputScope)
		return struct {
			src []byte
			err error
		}{src, err}
	})
	for i, read := range readSources {
		stats.FilesRead++
		if read.err != nil {
			log.Printf("[ts-extractor] error reading %s: %v", toRead[i], read.err)
			continue
		}
		sources[toRead[i]] = read.src
	}
	tr.Mark("ts_read_dirty", fmt.Sprintf("to_read=%d cached=%d", len(toRead), stats.CachedFiles))

	// GraphQL + gRPC indexes from cached summaries plus newly read files.
	graphqlServer := graphqlServerContext{sdlDocuments: map[string]bool{}}
	grpcIdx := newGRPCStubIndex()
	freshGQL := make(map[string]graphqlFileContribution, len(toRead))
	freshGRPC := make(map[string]*GRPCRecord, len(toRead))
	for _, rel := range tsFiles {
		if src, ok := sources[rel]; ok {
			g := collectGraphQLContribution(rel, src)
			if possibleGraphQLServerSignal(src) && !isGraphQLDocFile(rel) && !facts.IsTestPath(rel) {
				stats.GraphQLParsed++
			}
			freshGQL[rel] = g
			if g.Server {
				graphqlServer.enabled = true
			}
			for _, s := range g.SDL {
				graphqlServer.sdlDocuments[s] = true
			}
			rec := grpcFileContribution(src)
			freshGRPC[rel] = rec
			grpcIdx.mergeFile(rec)
			continue
		}
		if rec := prev[rel]; rec != nil {
			if rec.GraphQLServer {
				graphqlServer.enabled = true
			}
			for _, s := range rec.GraphQLSDL {
				graphqlServer.sdlDocuments[s] = true
			}
			grpcIdx.mergeFile(rec.GRPC)
		}
	}
	if grpcIdx.empty() {
		grpcIdx = nil
	}

	nuxtAutoByPkg := map[string]map[string]string{}
	for _, p := range nuxtPkgs {
		nuxtAutoByPkg[p] = nuxtAutoComponentIndex(knownFiles, p, nuxtPkgs)
	}

	type fileOut struct {
		res tsFileResult
		rec *FileRecord
	}
	tr.Mark("ts_graphql_grpc_index", fmt.Sprintf("ts_files=%d", len(tsFiles)))
	exportCache := newNamedExportCache()
	perFile := parallel.MapFiles(ctx, tsFiles, func(relFile string) fileOut {
		if !need(relFile) {
			rec := prev[relFile]
			return fileOut{res: resultFromRecord(rec), rec: rec}
		}
		src := sources[relFile]
		rec := &FileRecord{File: relFile}
		if src == nil {
			rec.Unreadable = true
			return fileOut{rec: rec}
		}
		sum := sha256.Sum256(src)
		rec.Hash = hex.EncodeToString(sum[:])
		if isMinifiedSource(src) {
			log.Printf("[ts-extractor] skipping minified/bundled file %s", relFile)
			rec.Minified = true
			return fileOut{rec: rec}
		}
		if hooks.OnBeforeParse != nil {
			hooks.OnBeforeParse(relFile)
		}
		aliases := mergePackageAliases(aliasesForDir(aliasRoots, factpath.Dir(relFile)), pkgAliases)
		fileNuxt, inNuxt := nuxtPackageForFile(nuxtPkgs, relFile)
		var auto map[string]string
		if inNuxt {
			auto = nuxtAutoByPkg[fileNuxt]
		}
		var res tsFileResult
		sideReads := map[string]bool{}
		readSrc := func(rel string) []byte {
			if b, ok := sources[rel]; ok {
				return b
			}
			raw, err := overlayReadFile(ctx, filepath.Join(repoPath, rel), inputScope)
			if err != nil {
				return nil
			}
			return raw
		}
		fileOrms, fileVue := pkgGates.forFile(pkgNamesEarly, relFile)
		res.facts, res.angular, res.angularRouter, res.angularInline, res.angularHTTP, res.clients = e.extractFile(src, relFile, isNextJS, fileVue, inNuxt, isSvelteKit, isEmber, isReactNav, isAngular, graphqlServer, fileOrms, aliases, knownFiles, readSrc, auto, grpcIdx, exportCache, sideReads)
		rec.AutoImportDirs = addImportsDirsFromFile(relFile, src)
		if len(sideReads) > 0 {
			rec.SideReads = make([]string, 0, len(sideReads))
			rec.SideReadHashes = make(map[string]string, len(sideReads))
			for f := range sideReads {
				rec.SideReads = append(rec.SideReads, f)
				b := readSrc(f)
				sum := sha256.Sum256(b)
				rec.SideReadHashes[f] = hex.EncodeToString(sum[:])
			}
			sort.Strings(rec.SideReads)
		}
		if !facts.IsTestPath(relFile) {
			res.routers = collectRouterFile(src, relFile, aliases, knownFiles)
		}
		statsKind := parseKind(relFile)
		if statsKind == "vue" || statsKind == "svelte" {
			// counted after merge; workers must not race on stats
		}
		fillRecord(rec, res, knownFiles, freshGQL[relFile], freshGRPC[relFile], statsKind)
		// After fillRecord, because eligibility is read off the finished record.
		// A surface is only ever consulted for a file that binds nothing - one
		// that cannot be a link in anyone's re-export chain - so indexing the rest
		// would buy nothing and cost a scan each; they keep an unrecorded surface,
		// which reads as unknown and never proves anything. Through exportCache
		// rather than around it: the index recorded here is the same object the
		// binder consults for this file, so a record cannot describe a surface the
		// binder did not use, and the scan is counted in SummaryScans like every
		// other. Indexing independently would have hidden both - a second full
		// parse per file, uncounted, and a second answer nothing reconciles.
		if rec.BindsNoImports() {
			rec.ExportSurface = exportCache.index(relFile, readSrc, aliases, knownFiles).surface()
			rec.ExportSurfaceRecorded = true
		}
		if hooks.OnFileLocal != nil {
			hooks.OnFileLocal(rec)
		}
		return fileOut{res: res, rec: rec}
	})

	records := make(map[string]*FileRecord, len(tsFiles))
	var allFacts []facts.Fact
	modules := make(map[string]bool)
	routerFiles := make([]*routerFile, 0, len(perFile))
	var angular angularCounts
	var angularRouters []*angularRouterFile
	var angularHTTPFiles []*angularHTTPFile
	clientTotals := clientCounts{}
	var angularRoutes, angularRequests angularCounts
	inlineTemplates := map[string]*angularTemplate{}
	parsed := 0
	sfc := 0
	for i, out := range perFile {
		rel := tsFiles[i]
		if out.rec != nil {
			records[rel] = out.rec
			if need(rel) && !out.rec.Unreadable && !out.rec.Minified {
				parsed++
				if out.rec.ParseKind == "vue" || out.rec.ParseKind == "svelte" {
					sfc++
				}
			}
		}
		res := out.res
		if res.routers != nil {
			routerFiles = append(routerFiles, res.routers)
		} else if out.rec != nil && out.rec.Router != nil {
			if rf := routerFromDTO(out.rec.Router); rf != nil {
				routerFiles = append(routerFiles, rf)
			}
		}
		angular.merge(res.angular)
		for name, tmpl := range res.angularInline {
			inlineTemplates[name] = tmpl
		}
		if res.angularRouter != nil {
			angularRouters = append(angularRouters, res.angularRouter)
		}
		if res.angularHTTP != nil {
			angularHTTPFiles = append(angularHTTPFiles, res.angularHTTP)
		}
		clientTotals.merge(res.clients)
		if len(res.facts) == 0 {
			continue
		}
		allFacts = append(allFacts, cloneFactSlice(res.facts)...)
		modules[factpath.Dir(rel)] = true
	}
	stats.FilesParsed = parsed
	stats.SFCParsed = sfc
	stats.SummaryScans = len(routerFiles) + len(angularRouters) + exportCache.summaryScans()
	tr.Mark("ts_mapfiles_aggregate", fmt.Sprintf("parsed=%d facts=%d routers=%d", parsed, len(allFacts), len(routerFiles)))

	if mounted := composeRouterMounts(routerFiles); len(mounted) > 0 {
		allFacts = append(allFacts, mounted...)
		for _, f := range mounted {
			modules[factpath.Dir(f.File)] = true
		}
	}

	if isNuxt {
		var extra []string
		seenDir := map[string]bool{}
		for _, rec := range records {
			if rec == nil {
				continue
			}
			for _, d := range rec.AutoImportDirs {
				if seenDir[d] {
					continue
				}
				seenDir[d] = true
				extra = append(extra, d)
			}
		}
		resolveNuxtAutoComposableCalls(allFacts, nuxtPkgs, extra, sources, invertPackageNames(collectPackageNames(repoPath, inputScope)))
	}
	applyDirectIOContract(allFacts)

	if isEmber {
		composeEngineMounts(allFacts)
	}
	if routes, c := composeAngularRoutes(angularRouters); len(routes) > 0 || c.total() > 0 {
		allFacts = append(allFacts, routes...)
		for _, f := range routes {
			modules[factpath.Dir(f.File)] = true
		}
		angularRoutes.merge(c)
	}

	templates := make(map[string]*angularTemplate, len(htmlFiles))
	var unreadable []string
	if len(htmlFiles) > 0 {
		type htmlOut struct {
			tmpl   *angularTemplate
			unread bool
		}
		scans := parallel.MapFiles(ctx, htmlFiles, func(relFile string) htmlOut {
			if !need(relFile) {
				return htmlOut{}
			}
			src, err := overlayReadFile(ctx, filepath.Join(repoPath, relFile), inputScope)
			if err != nil {
				log.Printf("[ts-extractor] error reading %s: %v", relFile, err)
				return htmlOut{unread: true}
			}
			return htmlOut{tmpl: scanAngularTemplate(src, relFile)}
		})
		for i, out := range scans {
			if out.unread {
				unreadable = append(unreadable, htmlFiles[i])
				continue
			}
			if out.tmpl != nil {
				templates[htmlFiles[i]] = out.tmpl
				stats.FilesRead++
				stats.FilesParsed++
			}
		}
	}
	angularTemplateCounts := attachAngularTemplates(allFacts, templates, inlineTemplates)
	if reqs, c := composeAngularRequests(angularHTTPFiles); len(reqs) > 0 || c.total() > 0 {
		allFacts = append(allFacts, reqs...)
		for _, f := range reqs {
			modules[factpath.Dir(f.File)] = true
		}
		angularRequests.merge(c)
	}
	if isAngular {
		angular.merge(reconcileAngularInjects(allFacts))
		angularRoutes.merge(resolveAngularLazyComponents(allFacts))
	}
	if angular.total() > 0 {
		if f, ok := extcoverage.Fact(repoPath, "typescript:angular-di", "angular_inject",
			angular.resolved, angular.unresolved); ok {
			allFacts = append(allFacts, f)
		}
	}
	if angularTemplateCounts.total() > 0 {
		if f, ok := extcoverage.Fact(repoPath, "typescript:angular-templates", "angular_template_ref",
			angularTemplateCounts.resolved, angularTemplateCounts.unresolved); ok {
			allFacts = append(allFacts, f)
		}
	}
	if angularRequests.total() > 0 {
		if f, ok := extcoverage.Fact(repoPath, "typescript:angular-requests", "angular_http_call",
			angularRequests.resolved, angularRequests.unresolved); ok {
			allFacts = append(allFacts, f)
		}
	}
	if angularRoutes.total() > 0 {
		if f, ok := extcoverage.Fact(repoPath, "typescript:angular-routes", "angular_route",
			angularRoutes.resolved, angularRoutes.unresolved); ok {
			allFacts = append(allFacts, f)
		}
	}
	if len(e.clients) > 0 {
		allFacts = append(allFacts, clientCoverageFacts(repoPath, e.clients, clientTotals)...)
	}
	if isPrisma {
		ff, unread := extractPrismaStorage(ctx, repoPath, inputScope)
		allFacts = append(allFacts, ff...)
		unreadable = append(unreadable, unread...)
	}
	pkgNames := pkgNamesEarly
	var projects map[string]string
	if isAngular {
		projects = angularProjectNames(repoPath, inputScope)
	}
	allFacts = appendTSDirectoryModules(allFacts, modules, pkgNames, projects)

	for _, rec := range records {
		if rec != nil && rec.Unreadable {
			unreadable = append(unreadable, rec.File)
		}
	}
	tr.Mark("ts_compose_modules", fmt.Sprintf("facts=%d records=%d unread=%d", len(allFacts), len(records), len(unreadable)))
	var configPaths []string
	if !hooks.SkipConfigPaths {
		configPaths = tsConfigInputs(repoPath, inputScope)
	}
	return &SessionResult{
		Facts:       allFacts,
		Records:     records,
		Stats:       stats,
		ConfigPaths: configPaths,
		Angular:     isAngular,
		Unreadable:  unreadable,
	}, nil
}

func parseKind(relFile string) string {
	switch {
	case isVueFile(relFile):
		return "vue"
	case isSvelteFile(relFile):
		return "svelte"
	case isGraphQLDocFile(relFile):
		return "graphql"
	case isHbsFile(relFile):
		return "hbs"
	default:
		return "ts"
	}
}

func fillRecord(rec *FileRecord, res tsFileResult, knownFiles map[string]bool, gql graphqlFileContribution, grpc *GRPCRecord, kind string) {
	rec.Facts = cloneFactSlice(res.facts)
	rec.ParseKind = kind
	rec.GraphQLServer = gql.Server
	rec.GraphQLSDL = gql.SDL
	rec.GRPC = grpc
	rec.Router = routerToDTO(res.routers)
	specs, resolved, declared, referenced, reexports, unresolved := summarizeFacts(res.facts, knownFiles)
	rec.ImportSpecs = specs
	rec.ResolvedFiles = resolved
	rec.Declared = declared
	rec.Referenced = referenced
	rec.Reexports = reexports
	rec.UnresolvedSpecs = unresolved
	rec.ImportComplete = true
}

func resultFromRecord(rec *FileRecord) tsFileResult {
	var res tsFileResult
	if rec == nil {
		return res
	}
	// Borrow immutable cached facts here. The aggregation loop clones them once
	// before composition can mutate properties/relations; cloning at both points
	// needlessly duplicates every unchanged file's facts on a small delta.
	res.facts = rec.Facts
	res.routers = routerFromDTO(rec.Router)
	return res
}

func summarizeFacts(ff []facts.Fact, knownFiles map[string]bool) (specs, resolved, declared, referenced, reexports, unresolved []string) {
	seenSpec := map[string]bool{}
	seenRes := map[string]bool{}
	seenRef := map[string]bool{}
	seenUn := map[string]bool{}
	for _, f := range ff {
		if f.Kind == facts.KindSymbol {
			declared = append(declared, f.Name)
		}
		if f.Kind == facts.KindDependency {
			if re, _ := f.Props["reexport"].(bool); re {
				reexports = append(reexports, f.Name)
			}
		}
		for _, r := range f.Relations {
			if !seenRef[r.Target] {
				seenRef[r.Target] = true
				referenced = append(referenced, r.Target)
			}
			if r.Kind != facts.RelImports {
				continue
			}
			spec := r.Target
			if replay := f.PropString(facts.PropImportSpec); replay != "" {
				spec = replay
			}
			if !seenSpec[spec] {
				seenSpec[spec] = true
				specs = append(specs, spec)
			}
			slash := filepath.ToSlash(spec)
			if tf := f.PropString(facts.PropTargetFile); tf != "" {
				tf = filepath.ToSlash(tf)
				if !seenRes[tf] {
					seenRes[tf] = true
					resolved = append(resolved, tf)
				}
				continue
			}
			if file, ok := NormalizeImportTarget(slash, knownFiles); ok {
				if !seenRes[file] {
					seenRes[file] = true
					resolved = append(resolved, file)
				}
				continue
			}
			src, _ := f.Props["source"].(string)
			if src == "external" || src == facts.DepSourceFramework {
				continue
			}
			if !seenUn[slash] {
				seenUn[slash] = true
				unresolved = append(unresolved, slash)
			}
		}
	}
	sort.Strings(specs)
	sort.Strings(resolved)
	sort.Strings(declared)
	sort.Strings(referenced)
	sort.Strings(reexports)
	sort.Strings(unresolved)
	return
}

// NormalizeImportTarget maps an extensionless or exact import target onto a
// known source file. ok is false when no owned file matches.
func NormalizeImportTarget(target string, knownFiles map[string]bool) (string, bool) {
	file, _, ok := resolveModuleFile(target, knownFiles)
	return file, ok
}

func internalImportTarget(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") {
		return true
	}
	if strings.HasPrefix(p, "@") {
		return false
	}
	return strings.Contains(p, "/")
}

func cloneFactSlice(in []facts.Fact) []facts.Fact {
	if len(in) == 0 {
		return nil
	}
	out := make([]facts.Fact, len(in))
	for i, f := range in {
		out[i] = cloneFact(f)
	}
	return out
}

func cloneFact(f facts.Fact) facts.Fact {
	cp := f
	if f.Props != nil {
		cp.Props = make(map[string]any, len(f.Props))
		for k, v := range f.Props {
			cp.Props[k] = v
		}
	}
	if f.Relations != nil {
		cp.Relations = make([]facts.Relation, len(f.Relations))
		copy(cp.Relations, f.Relations)
	}
	return cp
}

// ConfigInputPaths lists off-glob files that change TS extraction semantics.
func ConfigInputPaths(repoPath string, inputScopes ...*inputscope.Scope) []string {
	inputScope := inputscope.First(inputScopes)
	return tsConfigInputs(repoPath, inputScope)
}

// ConfigInputPathsFromNames keeps the original ConfigInputPaths traversal.
// Engine inventory pruning (ignore globs) is not equivalent to this walk.
func ConfigInputPathsFromNames(repoPath string, names []string, inputScopes ...*inputscope.Scope) []string {
	inputScope := inputscope.First(inputScopes)
	_ = names
	return tsConfigInputs(repoPath, inputScope)
}

// RepoUsesAngular reports whether Angular project markers are present.
func RepoUsesAngular(repoPath string, inputScopes ...*inputscope.Scope) bool {
	inputScope := inputscope.First(inputScopes)
	return detectAngular(repoPath, inputScope)
}

// SessionFiles is the incremental TypeScript unit of reanalysis: source files
// ExtractSession actually records, plus Angular templates when the repo uses
// Angular. OwnsFile is a cache-key superset and includes non-Angular HTML;
// those paths must not be treated as missing FileRecords.
func SessionFiles(files []string, angular bool) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if IsSessionSource(f, angular) {
			out = append(out, f)
		}
	}
	return out
}

// IsSessionSource reports whether rel is a file ExtractSession persists.
func IsSessionSource(rel string, angular bool) bool {
	if isTypeScriptFile(rel) {
		return true
	}
	return angular && isAngularTemplateFile(rel)
}

func tsConfigInputs(repoPath string, inputScopes ...*inputscope.Scope) []string {
	inputScope := inputscope.First(inputScopes)
	tCfg := time.Now()
	names := []string{
		"tsconfig.json", "tsconfig.base.json", "jsconfig.json",
		"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"deno.json", "deno.jsonc", "svelte.config.js", "svelte.config.ts", "svelte.config.mjs",
		"angular.json", "nx.json", "schema.prisma", filepath.Join("prisma", "schema.prisma"),
		"nuxt.config.ts", "nuxt.config.js", "nuxt.config.mjs", "next.config.js", "next.config.mjs", "next.config.ts",
	}
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	for _, n := range names {
		p := filepath.Join(repoPath, n)
		if _, err := inputScope.Stat(p); err == nil {
			add(n)
			if n == "tsconfig.json" || n == "tsconfig.base.json" || n == "jsconfig.json" {
				followTSConfigExtends(repoPath, p, add, inputScope)
			}
		} else {
			// Keep a stable slot so a later appearance still changes the fingerprint.
			add(n)
		}
	}
	// Framework detectors also inspect the selected TS root. Retain missing
	// candidates so config additions enter the resident reconciliation path.
	if tsRoot, found := findTSRoot(repoPath, inputScope); found {
		rel, err := filepath.Rel(repoPath, tsRoot)
		rel = factpath.Slash(rel)
		if err == nil {
			for _, family := range []string{"svelte", "nuxt", "next"} {
				for _, ext := range []string{"js", "ts", "mjs"} {
					add(factpath.Join(rel, family+".config."+ext))
				}
			}
		}
	}
	// Match the actual alias reader's roots, including inherited tsconfig paths.
	// The repository-root fallback is already represented by names above.
	for _, root := range collectTSAliasRoots(context.Background(), repoPath, inputScope) {
		for _, name := range []string{"svelte.config.js", "svelte.config.ts", "svelte.config.mjs"} {
			add(factpath.Join(root.dir, name))
		}
	}
	_ = inputScope.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || tsSkipDirs[name] {
				if path != repoPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch d.Name() {
		case "tsconfig.json", "tsconfig.base.json", "jsconfig.json", "package.json":
			rel, relErr := filepath.Rel(repoPath, path)
			if relErr != nil {
				return nil
			}
			rel = factpath.Slash(rel)
			add(rel)
			if d.Name() != "package.json" {
				followTSConfigExtends(repoPath, path, add, inputScope)
			}
		}
		return nil
	})
	sort.Strings(out)
	graphprofile.Since("ts_config_inputs", tCfg, fmt.Sprintf("n=%d", len(out)))
	return out
}

func followTSConfigExtends(repoPath, tsconfigPath string, add func(string), inputScopes ...*inputscope.Scope) {
	inputScope := inputscope.First(inputScopes)
	path := filepath.Clean(tsconfigPath) //factpath:host
	seen := map[string]bool{}
	for depth := 0; depth < 32; depth++ {
		if seen[path] {
			return
		}
		seen[path] = true
		data, err := overlayReadFile(context.Background(), path, inputScope)
		if err != nil {
			rel, relErr := filepath.Rel(repoPath, path)
			if relErr != nil {
				rel = path
			} else {
				rel = factpath.Slash(rel)
			}
			add(rel)
			return
		}
		var cfg struct {
			Extends string `json:"extends"`
		}
		if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
			return
		}
		next := resolveTSConfigExtends(path, strings.TrimSpace(cfg.Extends))
		if next == "" {
			return
		}
		rel, relErr := filepath.Rel(repoPath, next)
		if relErr != nil {
			rel = next
		} else {
			rel = factpath.Slash(rel)
		}
		add(rel)
		path = next
	}
}

// CompositionSignature is a stable hash of GraphQL/gRPC/Nuxt inputs that
// change extraction of otherwise-unchanged files. Dirty files are re-read so
// a context change is visible before BeginReplace.
func CompositionSignature(repoPath string, files []string, prev map[string]*FileRecord, dirty map[string]bool, sources map[string][]byte, inputScopes ...*inputscope.Scope) (string, error) {
	inputScope := inputscope.First(inputScopes)
	if prev == nil {
		prev = map[string]*FileRecord{}
	}
	ctx := withFileOverlay(context.Background(), newFileOverlay(repoPath, sources))
	allDirty := dirty == nil
	known := map[string]bool{}
	gql := graphqlServerContext{sdlDocuments: map[string]bool{}}
	grpcIdx := newGRPCStubIndex()
	for _, rel := range files {
		if !isTypeScriptFile(rel) {
			if isVueFile(rel) {
				known[filepath.ToSlash(rel)] = true
			}
			continue
		}
		known[filepath.ToSlash(rel)] = true
		var src []byte
		var err error
		usePrev := !allDirty && !dirty[rel] && prev[rel] != nil
		if usePrev {
			rec := prev[rel]
			if rec.GraphQLServer {
				gql.enabled = true
			}
			for _, s := range rec.GraphQLSDL {
				gql.sdlDocuments[s] = true
			}
			grpcIdx.mergeFile(rec.GRPC)
			continue
		}
		if sources != nil {
			if captured, ok := sources[rel]; ok {
				src = captured
			}
		}
		if src == nil {
			src, err = overlayReadFile(ctx, filepath.Join(repoPath, rel), inputScope)
			if err != nil {
				return "", err
			}
		}
		g := collectGraphQLContribution(rel, src)
		if g.Server {
			gql.enabled = true
		}
		for _, s := range g.SDL {
			gql.sdlDocuments[s] = true
		}
		grpcIdx.mergeFile(grpcFileContribution(src))
	}
	var sdl []string
	for s := range gql.sdlDocuments {
		sdl = append(sdl, s)
	}
	sort.Strings(sdl)
	var grpcParts []string
	if grpcIdx != nil {
		for name, svc := range grpcIdx.byService {
			fq := ""
			var methods []string
			if svc != nil {
				fq = svc.fq
				for m := range svc.methods {
					methods = append(methods, m)
				}
			}
			sort.Strings(methods)
			grpcParts = append(grpcParts, "svc:"+name+"="+fq+":"+strings.Join(methods, ","))
		}
		for name, svc := range grpcIdx.byClass {
			fq := ""
			var methods []string
			if svc != nil {
				fq = svc.fq
				for m := range svc.methods {
					methods = append(methods, m)
				}
			}
			sort.Strings(methods)
			grpcParts = append(grpcParts, "class:"+name+"="+fq+":"+strings.Join(methods, ","))
		}
		for name := range grpcIdx.ambiguousService {
			grpcParts = append(grpcParts, "amb-svc:"+name)
		}
		for name := range grpcIdx.ambiguousClass {
			grpcParts = append(grpcParts, "amb-class:"+name)
		}
	}
	sort.Strings(grpcParts)
	var nuxt []string
	nuxtPkgs := collectNuxtPackages(ctx, repoPath, inputScope)
	for _, pkg := range nuxtPkgs {
		for n, t := range nuxtAutoComponentIndex(known, pkg, nuxtPkgs) {
			nuxt = append(nuxt, pkg+"/"+n+"="+t)
		}
	}
	sort.Strings(nuxt)
	extraSeen := map[string]bool{}
	var extraDirs []string
	addExtra := func(d string) {
		if d == "" || extraSeen[d] {
			return
		}
		extraSeen[d] = true
		extraDirs = append(extraDirs, d)
	}
	for _, rec := range prev {
		if rec == nil {
			continue
		}
		for _, d := range rec.AutoImportDirs {
			addExtra(d)
		}
	}
	for rel, src := range sources {
		if src == nil {
			continue
		}
		if !allDirty && dirty != nil && !dirty[rel] {
			continue
		}
		for _, d := range addImportsDirsFromFile(rel, src) {
			addExtra(d)
		}
	}
	var auto []string
	for _, rel := range files {
		if !nuxtAutoImportDir(rel, nuxtPkgs, extraDirs) {
			continue
		}
		usePrev := !allDirty && dirty != nil && !dirty[rel] && prev[rel] != nil
		if usePrev {
			for _, n := range prev[rel].Declared {
				auto = append(auto, rel+"="+n)
			}
			continue
		}
		src := []byte(nil)
		if sources != nil {
			src = sources[rel]
		}
		if src == nil {
			raw, err := overlayReadFile(ctx, filepath.Join(repoPath, rel), inputScope)
			if err != nil {
				return "", err
			}
			src = raw
		}
		sum := sha256.Sum256(src)
		auto = append(auto, rel+"="+hex.EncodeToString(sum[:]))
	}
	sort.Strings(auto)
	h := sha256.New()
	if gql.enabled {
		h.Write([]byte("gql-on"))
	} else {
		h.Write([]byte("gql-off"))
	}
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sdl, ",")))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(grpcParts, ";")))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(nuxt, ";")))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(auto, ";")))
	return hex.EncodeToString(h.Sum(nil)), nil
}
