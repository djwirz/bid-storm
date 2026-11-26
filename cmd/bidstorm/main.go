// Command bidstorm storms a local auction target on a seeded schedule and records what
// happens. See README.md.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/proxy"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/schedule"
	"github.com/djwirz/bid-storm/internal/storm"
	"github.com/djwirz/bid-storm/internal/stream"
	"github.com/djwirz/bid-storm/internal/target"
	"github.com/djwirz/bid-storm/internal/target/holdfast"
)

// The local placeholder from Holdfast's docker-compose.yml.
const localDatabase = "postgres://holdfast:holdfast@localhost:5432/holdfast"

// errNotUsable means the run happened and was saved, but shouldn't be used as it is.
var errNotUsable = errors.New("the recording is kept, but the run is not usable")

func main() {
	// Ctrl-C cancels ctx, so a stopped run still gets its manifest.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch err := run(ctx, os.Args[1:]); {
	case errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errNotUsable):
		fmt.Fprintln(os.Stderr, "bidstorm:", err)
		os.Exit(3)
	case err != nil:
		fmt.Fprintln(os.Stderr, "bidstorm:", err)
		os.Exit(1)
	}
}

type options struct {
	url, origin, checkout, node, database, fakeEnv, out, scheduleFile string
	proxyListen, fakeURL                                              string

	proxy        bool
	subscribers  int
	drainTimeout time.Duration

	params          schedule.Params
	duration        time.Duration
	auction         time.Duration
	lagLimit        time.Duration
	maxInFlight     int
	priceEvery      time.Duration
	pollEvery       time.Duration
	pollTimeout     time.Duration
	requestsTimeout time.Duration
}

func parse(args []string) (options, error) {
	env := func(name, fallback string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return fallback
	}
	home, _ := os.UserHomeDir()
	var o options
	f := flag.NewFlagSet("bidstorm", flag.ContinueOnError)
	f.StringVar(&o.url, "url", "http://localhost:3000", "target base URL (this machine only)")
	f.StringVar(&o.origin, "origin", "", "Origin header, the target's APP_ORIGIN (default: -url)")
	f.StringVar(&o.checkout, "holdfast", env("HOLDFAST_DIR", filepath.Join(home, "projects", "holdfast")), "Holdfast checkout, for creating the auction (env HOLDFAST_DIR)")
	f.StringVar(&o.node, "node", env("NODE", "node"), "Node 26 binary (env NODE)")
	f.StringVar(&o.database, "database", "", "Holdfast's database, this machine only (default: env DATABASE_URL, else Holdfast's docker compose one)")
	f.StringVar(&o.fakeEnv, "fake-env", "targets/holdfast/fake.env", "the fake's declared settings, recorded in the manifest")
	f.StringVar(&o.out, "out", "runs", "directory for recordings")
	f.StringVar(&o.scheduleFile, "schedule", "", "send a saved schedule.json again (ignores the five flags that shape one)")
	f.Uint64Var(&o.params.Seed, "seed", 1, "schedule seed")
	f.Float64Var(&o.params.Rate, "rate", 50, "mean bids per second")
	f.DurationVar(&o.duration, "duration", 10*time.Second, "how long bids are sent")
	f.IntVar(&o.params.Bidders, "bidders", 30, "number of bidders")
	f.Float64Var(&o.params.RetryRate, "retry-rate", 0.1, "share of sends that reuse an earlier key")
	f.Float64Var(&o.params.ConflictRate, "conflict-rate", 0.3, "share of retries that change the amount")
	f.DurationVar(&o.auction, "auction", 8*time.Second, "auction length; shorter than -duration so bids hit the close")
	f.DurationVar(&o.lagLimit, "max-lag", 50*time.Millisecond, "a send later than this invalidates the run")
	f.IntVar(&o.maxInFlight, "max-in-flight", 2000, "bids awaiting a reply before sends are skipped")
	f.DurationVar(&o.priceEvery, "price-every", 100*time.Millisecond, "how often the current price is read")
	f.DurationVar(&o.pollEvery, "poll-every", 500*time.Millisecond, "how often a pending bid is polled")
	f.DurationVar(&o.pollTimeout, "poll-timeout", 60*time.Second, "when to give up on a pending bid")
	f.DurationVar(&o.requestsTimeout, "timeout", 30*time.Second, "per-request timeout")
	f.BoolVar(&o.proxy, "proxy", true, "record holds; with -proxy=false the target must call the fake directly")
	f.StringVar(&o.proxyListen, "proxy-listen", "127.0.0.1:4011", "where the recording proxy listens: the target's FAKE_PAYMENTS_URL")
	f.StringVar(&o.fakeURL, "fake-url", "http://127.0.0.1:4010", "the Stripe fake the proxy forwards to (this machine only)")
	f.IntVar(&o.subscribers, "subscribers", 4, "live-stream subscribers")
	f.DurationVar(&o.drainTimeout, "drain-timeout", 90*time.Second, "how long to wait for the close and every hold to be settled")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	// Not a flag default: -h prints those, and this holds a password.
	if o.database == "" {
		o.database = env("DATABASE_URL", localDatabase)
	}
	if o.origin == "" {
		o.origin = strings.TrimRight(o.url, "/")
	}
	if o.auction < time.Second || o.priceEvery <= 0 || o.pollEvery <= 0 || o.maxInFlight <= 0 {
		return o, errors.New("-auction must be at least 1s; intervals and -max-in-flight must be positive")
	}
	if o.subscribers < 0 || o.drainTimeout <= 0 {
		return o, errors.New("-subscribers can't be negative and -drain-timeout must be positive")
	}
	if err := target.CheckLoopback("http://" + o.proxyListen); o.proxy && err != nil {
		return o, fmt.Errorf("-proxy-listen: %w", err)
	}
	o.params.DurationUS = o.duration.Microseconds()
	return o, nil
}

func run(ctx context.Context, args []string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}

	from := "generated"
	var plan schedule.Schedule
	if o.scheduleFile != "" {
		from = "file"
		plan, err = schedule.Load(o.scheduleFile)
	} else {
		plan, err = schedule.Generate(o.params)
	}
	if err != nil {
		return err
	}
	if len(plan.Entries) == 0 {
		return errors.New("the schedule has no sends; raise -rate or -duration")
	}
	fake, err := readFakeEnv(o.fakeEnv)
	if err != nil {
		return err
	}

	hc := contract.NewHTTPClient(o.requestsTimeout)
	adapter, err := holdfast.New(hc, o.url, o.checkout, o.node, o.database)
	if err != nil {
		return err
	}
	if err := adapter.Preflight(ctx); err != nil {
		return err
	}
	commit, dirty, err := adapter.Revision(ctx)
	if err != nil {
		return err
	}
	client := contract.New(hc, o.url, o.origin, adapter.InstanceHeader())

	// Up before anything is bid: the target's holds go through it.
	var holds *proxy.Proxy
	var proxyServer *http.Server
	if o.proxy {
		if holds, proxyServer, err = startProxy(o.proxyListen, o.fakeURL); err != nil {
			return err
		}
		defer proxyServer.Close()
	}

	// Sign in before the auction exists, so its clock isn't spent on this.
	sessions, err := signIn(ctx, client, plan.Params.Bidders)
	if err != nil {
		return err
	}

	runID := fmt.Sprintf("%s-s%d", time.Now().UTC().Format("20060102T150405.000Z"), plan.Params.Seed)
	auctionID, err := adapter.CreateAuction(ctx, "bid-storm "+runID, o.auction)
	if err != nil {
		return err
	}

	dir := filepath.Join(o.out, runID)
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	if err := plan.Save(filepath.Join(dir, "schedule.json")); err != nil {
		return err
	}
	events, err := record.NewWriter(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return err
	}
	defer events.Close() // for early returns; the real close below reports the error

	clock := record.NewClock()
	manifest := record.Manifest{
		SchemaVersion: record.SchemaVersion,
		RunID:         runID,
		StartedUS:     clock.US(time.Now()),
		Schedule:      plan.Params,
		ScheduleFrom:  from,
		Settings: record.Settings{
			AuctionSeconds: int((o.auction + time.Second - 1) / time.Second),
			LagLimitUS:     o.lagLimit.Microseconds(), MaxInFlight: o.maxInFlight,
			PriceEveryUS: o.priceEvery.Microseconds(), PollEveryUS: o.pollEvery.Microseconds(),
			PollTimeoutUS: o.pollTimeout.Microseconds(), RequestTimeoutUS: o.requestsTimeout.Microseconds(),
			Proxy: o.proxy, Subscribers: o.subscribers, DrainTimeoutUS: o.drainTimeout.Microseconds(),
		},
		Target: record.Target{
			Name: adapter.Name(), URL: o.url, AuctionID: auctionID,
			Commit: commit, CommitDirty: dirty, FakeDeclared: fake, InstancesSeen: []string{},
		},
		Machine:        record.Machine{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()},
		Generator:      generator(),
		InvalidReasons: []string{"the run did not finish"},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	// Written now too, so a crash leaves a recording that says it is incomplete.
	if err := manifest.Save(manifestPath); err != nil {
		return err
	}

	if holds != nil {
		holds.Record(events, clock, auctionID)
	}
	subscribers, err := stream.Subscribe(ctx, client, auctionID, o.subscribers, events, clock)
	if err != nil {
		return err
	}
	defer subscribers.Stop()

	fmt.Printf("run %s: %d bids from %d bidders over %s, auction %s\n",
		runID, len(plan.Entries), plan.Params.Bidders, time.Duration(plan.Params.DurationUS)*time.Microsecond, auctionID)
	outcome, runErr := storm.Run(ctx, storm.Config{
		Schedule: plan, AuctionID: auctionID, Sessions: sessions, MaxInFlight: o.maxInFlight,
		PriceEvery: o.priceEvery, PollEvery: o.pollEvery, PollTimeout: o.pollTimeout,
	}, client, events, clock)

	// The target still has the close to announce and holds to release or capture.
	results := outcome.Results
	waitFor := []func() string{subscribers.Waiting}
	if holds != nil {
		waitFor = append(waitFor, func() string {
			if n := holds.Outstanding(); n > 0 {
				return fmt.Sprintf("%d holds not yet answered, released or captured", n)
			}
			return ""
		})
	}
	drainStart := time.Now()
	results.DrainWaiting = []string{}
	if runErr == nil {
		results.DrainWaiting = storm.Drain(ctx, o.drainTimeout, waitFor...)
		runErr = ctx.Err()
	}
	results.DrainUS = time.Since(drainStart).Microseconds()

	// The target's checker runs while the proxy is still up, so anything the target has left
	// to settle can still reach the fake and be recorded.
	var evidence record.EndOfRun
	var check record.Check
	if runErr == nil {
		if check, err = adapter.Check(ctx, auctionID, o.fakeURL); err != nil {
			fmt.Fprintln(os.Stderr, "bidstorm: no verdict from the target's checker:", err)
		} else if err := record.SaveJSON(filepath.Join(dir, "check.json"), check); err != nil {
			return err
		} else {
			evidence.CheckSaved = true
		}
		runErr = ctx.Err()
	}

	// Everything that writes events stops before the file closes.
	seen := subscribers.Stop()
	var held map[string]bool
	if holds != nil {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := proxyServer.Shutdown(shutdown); err != nil {
			proxyServer.Close()
		}
		held, results.HoldCalls = holds.Held()
	}
	// After the checker, which waits for the target to be quiet.
	if runErr == nil {
		intents, err := proxy.Audit(ctx, o.fakeURL, auctionID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bidstorm:", err)
		} else {
			audit := record.Audit{V: record.SchemaVersion, AuctionID: auctionID, FetchedUS: clock.US(time.Now()), Intents: intents}
			if err := record.SaveJSON(filepath.Join(dir, "audit.json"), audit); err != nil {
				return err
			}
			evidence.AuditSaved = true
		}
	}
	results.StreamEvents, results.StreamReconnects = seen.Events, seen.Reconnects
	results.Timeline = storm.CheckTimeline(outcome.Accepted, held, seen.Announced)
	writeErr := events.Close()

	manifest.EndedUS = clock.US(time.Now())
	manifest.Results = results
	manifest.EndOfRun = evidence
	manifest.Target.InstancesSeen = append(manifest.Target.InstancesSeen, outcome.Instances...)
	manifest.Target.InstancesSeen = append(manifest.Target.InstancesSeen, seen.Instances...)
	slices.Sort(manifest.Target.InstancesSeen)
	manifest.Target.InstancesSeen = slices.Compact(manifest.Target.InstancesSeen)
	manifest.Complete = runErr == nil && writeErr == nil && len(results.DrainWaiting) == 0
	manifest.InvalidReasons = storm.InvalidReasons(results, o.lagLimit, runErr, writeErr)
	manifest.Valid = len(manifest.InvalidReasons) == 0
	if err := manifest.Save(manifestPath); err != nil {
		return err
	}

	summarize(os.Stdout, dir, manifest)
	// Shown, not judged: `make check` is the gate (ADR 0008).
	if evidence.CheckSaved {
		fmt.Printf("target's checker: exit %d, %d violations\n", check.ExitCode, len(check.Violations))
	}
	switch {
	case runErr != nil:
		return fmt.Errorf("%w: %w", errNotUsable, runErr)
	case writeErr != nil:
		return fmt.Errorf("%w: %w", errNotUsable, writeErr)
	case !manifest.Valid:
		return errNotUsable
	case results.AcceptedBids == 0:
		return fmt.Errorf("%w: no bid was accepted", errNotUsable)
	}
	return nil
}

func summarize(w io.Writer, dir string, m record.Manifest) {
	r := m.Results
	ms := func(us int64) time.Duration { return time.Duration(us) * time.Microsecond }
	fmt.Fprintf(w, "sent %d of %d, accepted %d, polls %d, unresolved %d\n", r.Sent, r.Scheduled, r.AcceptedBids, r.Polls, r.UnresolvedBids)
	fmt.Fprintf(w, "HTTP %v\nbids by last status seen %v\n", r.HTTPStatuses, r.LastStatuses)
	if len(r.TransportErrors) > 0 || r.PriceReadErrors > 0 {
		fmt.Fprintf(w, "transport errors %v, price read errors %d\n", r.TransportErrors, r.PriceReadErrors)
	}
	fmt.Fprintf(w, "holds %v, stream events %d, reconnects %d, waited %s for the target\n",
		r.HoldCalls, r.StreamEvents, r.StreamReconnects, ms(r.DrainUS).Round(time.Millisecond))
	fmt.Fprintf(w, "send lag p99 %s, max %s; instances %v\n", ms(r.SendLagP99US), ms(r.SendLagMaxUS), m.Target.InstancesSeen)
	for _, reason := range m.InvalidReasons {
		fmt.Fprintln(w, "INVALID:", reason)
	}
	fmt.Fprintln(w, "recording:", dir)
}

// startProxy listens for the target's calls to the fake.
func startProxy(listen, fakeURL string) (*proxy.Proxy, *http.Server, error) {
	p, err := proxy.New(fakeURL)
	if err != nil {
		return nil, nil, err
	}
	l, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, nil, fmt.Errorf("recording proxy: %w", err)
	}
	server := &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(l) // returns when the server is shut down
	return p, server, nil
}

// signIn gets one session per bidder, a few at a time. The same logins are reused by every run.
func signIn(ctx context.Context, client *contract.Client, n int) ([]contract.Session, error) {
	sessions := make([]contract.Session, n)
	errs := make([]error, n) // each goroutine writes only its own index
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range n {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			sessions[i], errs[i] = client.SignIn(ctx, fmt.Sprintf("storm-%d", i))
		})
	}
	wg.Wait()
	return sessions, errors.Join(errs...)
}

// readFakeEnv takes only FAKE_* lines, so nothing else in the file can reach a recording.
func readFakeEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fake settings: %w", err)
	}
	defer f.Close()
	out := map[string]string{}
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		name, value, found := strings.Cut(strings.TrimSpace(lines.Text()), "=")
		if found && strings.HasPrefix(name, "FAKE_") {
			out[name] = value
		}
	}
	return out, lines.Err()
}

// generator reads the build information Go stamps into the binary.
func generator() record.Generator {
	g := record.Generator{GoVersion: runtime.Version(), Revision: "unknown"}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				g.Revision = s.Value
			case "vcs.modified":
				g.Dirty = s.Value == "true"
			}
		}
	}
	return g
}
