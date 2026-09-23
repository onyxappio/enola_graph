package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/bootstrap"
)

// Graph is `enola graph`: streaming initial and file-granularity delta analysis.
// It publishes file-owned replacements through a sink (NATS JetStream or a file)
// and does not run explainers or write snapshot artifacts.
func (r *Runner) Graph(ctx context.Context, args []string) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(os.Stderr,
			"Usage: "+r.name()+" graph <analyze|delta|watch|fork> [flags] [repo_path|config_path]\n\n"+
				"Graph-only streaming analysis. analyze/delta/watch require --nats or --events.\n"+
				"  analyze  initial analysis (use --force-initial to ignore stored state)\n"+
				"  delta    incremental file-granularity update from stored state\n"+
				"  watch    resident event-driven analysis through observed watermarks\n"+
				"  fork     seed a NEW empty --state-dir/--context from --base-state-dir\n")
		os.Exit(0)
	}
	mode := args[0]
	switch mode {
	case "analyze", "delta", "watch", "fork":
	default:
		fmt.Fprintf(os.Stderr, "%s graph: unknown mode %q (expected analyze, delta, watch, or fork)\n", r.name(), mode)
		os.Exit(2)
	}

	fs := flag.NewFlagSet("graph "+mode, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		watchEvery    = fs.Duration("watch-every", graphsession.DefaultWatchEvery, "with watch: fixed change-collection window after the first event (positive duration, e.g. 5s or 10s)")
		authoritative = fs.Bool("authoritative-scope", false, "use frozen file-owner BeginReplace manifest (v2 protocol)")
		maxBeginBytes = fs.Int("max-begin-bytes", 0, "maximum BeginReplace payload bytes (0 = 256KiB, or 512KiB with --authoritative-scope). Frozen v2 refuses oversized manifests; it does not chunk owner scope. Must fit the broker max_payload.")
		natsURL       = fs.String("nats", "", "NATS URL (JetStream)")
		stream        = fs.String("stream", "ENOLA_GRAPH", "JetStream stream name")
		subject       = fs.String("subject", "enola.graph.>", "JetStream subject filter for the stream")
		events        = fs.String("events", "", "write protocol JSONL to this file (debug sink)")
		contextID     = fs.String("context", "default", "analysis context id (branch/workspace label, not a generation)")
		stateDir      = fs.String("state-dir", "", "durable graph state directory (default: <repo>/.enola/graphstate)")
		summaryJSON   = fs.Bool("summary-json", false, "print result summaries without Facts as a JSON array")
		configPath    = fs.String("config", "", "explicit graph configuration path")
		asJSON        = fs.Bool("json", false, "print the run result as JSON")
		force         = fs.Bool("force-initial", false, "with analyze: ignore existing state and run a full initial replacement")
		baseStateDir  = fs.String("base-state-dir", "", "completed source graphstate to seed a new --state-dir/--context (fork, or delta)")
		repoID        = fs.String("repo-id", "", "stable repository identity (default: absolute checkout path)")
	)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr,
			"Usage: "+r.name()+" graph "+mode+" [flags] [repo_path|config_path]\n\n"+
				"Graph-only analysis: extract facts and publish file-owned replacements.\n"+
				"No explainers, no llm_context.md, no database writes.\n\n"+
				"analyze/delta/watch require a durable sink: --nats or --events.\n"+
				"fork copies a completed checkpoint into an empty --state-dir under a new\n"+
				"--context and does not publish. Independent worktrees are not supported.\n"+
				"TypeScript/JavaScript is the incremental primary path. Other detected\n"+
				"extractors re-run as a whole extractor and the fallback is reported.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		os.Exit(2)
	}
	if *watchEvery <= 0 {
		r.cmdFatal("graph", "--watch-every must be a positive duration")
	}
	if *maxBeginBytes < 0 {
		r.cmdFatal("graph", "--max-begin-bytes must be >= 0")
	}
	arg := ""
	if rest := fs.Args(); len(rest) > 0 {
		arg = rest[0]
	}

	// Everything from process start to the first session trace was an untraced
	// residual in root's profile: config discovery, repository selection and
	// engine construction happen here, before any graph hop exists to attribute
	// them to. This trace is the outermost of the five and never nests inside
	// another, so its marks partition the CLI's own wall time.
	ctr := graphprofile.StartNamed("cli")
	ctr.Mark("flags_parsed", "mode="+mode)
	tgt := r.resolveGraphTarget(arg, *configPath, []string{*stateDir, *events})
	ctr.Mark("resolve_graph_target", fmt.Sprintf("repos=%d", len(tgt.repoPaths)))
	fmt.Fprintf(os.Stderr, r.name()+" graph: %s\n", tgt.configNote)
	tgt.engine.SetPersistCache(false)

	if mode == "fork" {
		if *baseStateDir == "" || *stateDir == "" {
			r.cmdFatal("graph", "fork requires --base-state-dir and --state-dir")
		}
		if *contextID == "" || *contextID == "default" {
			r.cmdFatal("graph", "fork requires a distinct --context (not default)")
		}
		if len(tgt.repoPaths) != 1 {
			r.cmdFatal("graph", "fork requires exactly one repository")
		}
		repo := tgt.repoPaths[0]
		abs, err := filepath.Abs(repo)
		if err != nil {
			r.cmdFatal("graph", "%v", err)
		}
		st, err := graphsession.Fork(graphsession.ForkOptions{
			SourceDir:        *baseStateDir,
			TargetDir:        *stateDir,
			ContextID:        *contextID,
			RepoID:           *repoID,
			Checkout:         abs,
			ExtractorVersion: engine.ExtractorVersion(),
		})
		if err != nil {
			r.cmdFatal("graph", "%v", err)
		}
		fmt.Fprintf(os.Stderr, "[graph] forked %s gen=%d run=%s → context %q state-dir %s\n",
			st.ForkBaseContextID, st.ForkBaseGeneration, st.ForkBaseRunID, st.ContextID, *stateDir)
		if *asJSON {
			out, err := json.MarshalIndent(st, "", "  ")
			if err != nil {
				r.cmdFatal("graph", "json: %v", err)
			}
			fmt.Println(string(out))
		}
		return
	}

	if *natsURL == "" && *events == "" {
		r.cmdFatal("graph", "require --nats or --events; refusing to advance graph state with no durable sink")
	}

	var sink graphstream.Sink
	if *natsURL != "" {
		natsOpts := graphstream.NATSOptions{
			URL:     *natsURL,
			Stream:  *stream,
			Subject: *subject,
		}
		if *maxBeginBytes > 512*1024 {
			natsOpts.MaxPayload = *maxBeginBytes
		}
		js, err := graphstream.ConnectNATS(ctx, natsOpts)
		if err != nil {
			r.cmdFatal("graph", "%v", err)
		}
		defer js.Close()
		sink = js
	} else {
		fsink, err := openEventFile(*events)
		if err != nil {
			r.cmdFatal("graph", "%v", err)
		}
		defer fsink.Close()
		sink = fsink
	}
	ctr.Mark("connect_sink", fmt.Sprintf("nats=%v", *natsURL != ""))

	optsFor := func(repo string) graphsession.Options {
		opts := graphsession.Options{
			AuthoritativeFiles: *authoritative,
			MaxBeginBytes:      *maxBeginBytes,
			WatchEvery:         *watchEvery,
			ContextID:          *contextID,
			StateDir:           *stateDir,
			RepoID:             *repoID,
			ForceInitial:       mode == "analyze" && *force,
			Subject:            *subject,
			// tgt.engine was constructed by resolveGraphTarget a few statements
			// ago, in this process, and nothing has run against it. That is the
			// whole of the claim; the session still has to prove the policy's
			// declared inputs unmoved before it acts on it.
			FreshEngine: true,
		}
		if opts.StateDir == "" {
			opts.StateDir = filepath.Join(repo, tgt.engine.Config().Output.Dir, "graphstate")
		}
		if *natsURL != "" {
			opts.SinkID = "nats|" + *natsURL + "|" + *stream + "|" + *subject
		} else if *events != "" {
			absEv, err := filepath.Abs(*events)
			if err != nil {
				absEv = *events
			}
			opts.SinkID = "file|" + absEv
			opts.WatchIgnore = append(opts.WatchIgnore, absEv)
		}
		if mode == "watch" {
			opts.ConfigPaths = []string{tgt.cfgPath}
			opts.ReloadEngine = func(ctx context.Context) (*engine.Engine, error) {
				fresh := r.resolveGraphTarget(arg, *configPath, []string{*stateDir, *events})
				found := false
				for _, candidate := range fresh.repoPaths {
					a, _ := filepath.Abs(candidate)
					b, _ := filepath.Abs(repo)
					if a == b {
						found = true
					}
				}
				if !found {
					return nil, fmt.Errorf("watch config changed repository selection; reopen graph watch")
				}
				fresh.engine.SetPersistCache(false)
				return fresh.engine.Analysis(), nil
			}
		}
		return opts
	}

	if mode == "watch" {
		if len(tgt.repoPaths) == 0 {
			r.cmdFatal("graph", "no repository to watch")
		}
		errc := make(chan error, len(tgt.repoPaths))
		for _, repo := range tgt.repoPaths {
			repo := repo
			go func() {
				errc <- graphsession.Watch(ctx, tgt.engine.Analysis(), repo, sink, optsFor(repo))
			}()
		}
		for range tgt.repoPaths {
			if err := <-errc; err != nil {
				r.cmdFatal("graph", "%v", err)
			}
		}
		return
	}

	var results []*graphsession.Result
	for _, repo := range tgt.repoPaths {
		opts := optsFor(repo)
		ctr.Mark("session_options", "repo="+repo)
		if (mode == "delta" || mode == "analyze") && *baseStateDir != "" {
			if opts.StateDir == "" {
				r.cmdFatal("graph", "--base-state-dir requires --state-dir for the new context")
			}
			abs, err := filepath.Abs(repo)
			if err != nil {
				r.cmdFatal("graph", "%v", err)
			}
			if _, err := graphsession.Fork(graphsession.ForkOptions{
				SourceDir:        *baseStateDir,
				TargetDir:        opts.StateDir,
				ContextID:        opts.ContextID,
				RepoID:           opts.RepoID,
				SinkID:           opts.SinkID,
				Checkout:         abs,
				ExtractorVersion: engine.ExtractorVersion(),
			}); err != nil {
				r.cmdFatal("graph", "%v", err)
			}
		}
		res, err := graphsession.Run(ctx, tgt.engine.Analysis(), repo, sink, opts)
		if err != nil {
			r.cmdFatal("graph", "%s: %v", repo, err)
		}
		// Covers the whole of Run, which the open/session traces partition from
		// the inside; the cli trace only needs it as one block so that whatever
		// remains after it is genuinely CLI-side.
		ctr.Mark("graph_session_run", fmt.Sprintf("repo=%s parsed=%d", repo, res.ParsedFiles))
		results = append(results, res)
		fmt.Fprintf(os.Stderr, "[graph] %s: generation %d→%d parsed=%d cached=%d early_local=%d fallbacks=%d\n",
			repo, res.BaseGeneration, res.TargetGeneration, res.ParsedFiles, res.Stats.CachedFiles, res.EarlyLocal, len(res.Fallbacks))
		for _, f := range res.Fallbacks {
			fmt.Fprintf(os.Stderr, "[graph] fallback %s: %s (%s)\n", f.Extractor, f.Scope, f.Reason)
		}
	}
	if *asJSON || *summaryJSON {
		if *summaryJSON && !*asJSON {
			for i, res := range results {
				copy := *res
				copy.Facts = nil
				results[i] = &copy
			}
		}
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			r.cmdFatal("graph", "json: %v", err)
		}
		fmt.Println(string(out))
	}
	// Terminal mark: result marshalling of a full --json run is not free, and
	// without this it lands after the last mark and outside the trace.
	ctr.Mark("cli_complete", fmt.Sprintf("results=%d", len(results)))
}

type eventFileSink struct {
	f *os.File
}

func openEventFile(path string) (*eventFileSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &eventFileSink{f: f}, nil
}

func (s *eventFileSink) Publish(ctx context.Context, subject, msgID string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(s.f, "%s %s %s\n", subject, msgID, payload)
	if err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *eventFileSink) Flush(context.Context) error { return s.f.Sync() }
func (s *eventFileSink) Close() error                { return s.f.Close() }

func (r *Runner) resolveGraphTarget(arg, override string, outputs []string) target {
	repo := arg
	if repo == "" {
		repo = "."
	}
	if !isDirectory(repo) {
		if override != "" {
			r.cmdFatal("graph", "use a repository argument with --config")
		}
		override = repo
		cfg, err := config.Load(override)
		if err != nil {
			r.cmdFatal("graph", "%v", err)
		}
		roots, err := cfg.RepoPaths()
		if err != nil || len(roots) != 1 {
			r.cmdFatal("graph", "config must select exactly one repository")
		}
		repo = roots[0]
	}
	eng, err := bootstrap.NewGraphEngine(bootstrap.GraphOptions{Repo: repo, ConfigPath: override, StateDirs: outputs})
	if err != nil {
		r.cmdFatal("graph", "%v", err)
	}
	cfg := eng.Config()
	path := cfg.SourcePath
	if path == "" {
		path = filepath.Join(cfg.Repo, "mcp-arch.yaml")
	}
	note := "built-in graph defaults"
	if cfg.SourcePath != "" {
		note = "config " + cfg.SourcePath
	}
	return target{engine: eng, repoPaths: []string{cfg.Repo}, configNote: note, cfgPath: path}
}
