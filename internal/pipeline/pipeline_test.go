package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/jobs"
	"github.com/hneyra/crawler-go/internal/pipeline"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/storage"
)

type env struct {
	pool *pgxpool.Pool
	svc  *pipeline.Service
	q    postgres.JobQueue
	runs postgres.Runs
	site crawl.Site
	cat  crawl.Category
}

func newEnv(t *testing.T, siteURL string) *env {
	t.Helper()
	pool := dbtest.New(t)
	ctx := context.Background()
	sites, cats := postgres.Sites{Pool: pool}, postgres.Categories{Pool: pool}
	cfg := crawl.DefaultExtractionConfig()
	cfg.FieldSelectors["title"] = "h1"
	site, err := sites.CreateSite(ctx, crawl.Site{Name: "shop", BaseURL: siteURL, Enabled: true, ExtractionConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := cats.CreateCategory(ctx, crawl.Category{SiteID: site.ID, Name: "list", URL: siteURL + "/list", PaginationType: crawl.PaginationPageParam, ItemLinkSelector: "a.item", LinkExtractionType: crawl.LinkHref, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	urls := postgres.URLs{Pool: pool}
	f := &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0"}
	e := &env{pool: pool, q: postgres.JobQueue{Pool: pool}, runs: postgres.Runs{Pool: pool}, site: site, cat: cat}
	e.svc = &pipeline.Service{
		Tx: db.Runner{Pool: pool}, Runs: e.runs, Sites: sites, Categories: cats, Jobs: e.q,
		Discovery:  &discovery.Service{Fetcher: f, URLs: urls, Sleep: func(context.Context, time.Duration) error { return nil }},
		Extraction: &extraction.Service{Sites: sites, Queue: urls, Items: postgres.Items{Pool: pool}, Fetcher: f, Snapshots: storage.Raw{Dir: t.TempDir()}, BatchSize: 2, Sleep: func(context.Context, time.Duration) error { return nil }},
	}
	return e
}

func shop(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<a class="item" href="/p/1">1</a><a class="item" href="/p/2">2</a><a class="item" href="/p/3">3</a><a rel="next" href="/list2">n</a>`)
		case "/list2":
			_, _ = fmt.Fprint(w, `<a class="item" href="/p/4">4</a><a class="item" href="/gone">x</a>`)
		case "/gone":
			http.NotFound(w, r)
		default:
			_, _ = fmt.Fprintf(w, `<title>t</title><h1>Producto %s</h1>`, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestDedupesActiveRun(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	r1, dup, err := e.svc.Request(ctx, crawl.RunCrawlSite, e.site.ID, 0)
	if err != nil || dup || r1.Status != crawl.RunQueued || r1.JobID == 0 {
		t.Fatalf("first: %v %v %+v", err, dup, r1)
	}
	r2, dup, err := e.svc.Request(ctx, crawl.RunCrawlSite, e.site.ID, 0)
	if err != nil || !dup || r2.ID != r1.ID {
		t.Fatalf("dedupe: %v %v %+v", err, dup, r2)
	}
	// other kind / category = separate run.
	r3, dup, err := e.svc.Request(ctx, crawl.RunDiscoverCategory, 0, e.cat.ID)
	if err != nil || dup || r3.ID == r1.ID || r3.SiteID != e.site.ID || r3.CategoryID != e.cat.ID {
		t.Fatalf("category run: %v %+v", err, r3)
	}
	var n int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM crawl_run`).Scan(&n)
	if n != 2 {
		t.Fatalf("runs %d", n)
	}
	if _, _, err := e.svc.Request(ctx, crawl.RunExtractSite, 999, 0); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("missing site: %v", err)
	}
	if _, _, err := e.svc.Request(ctx, crawl.RunDiscoverCategory, 0, 999); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("missing category: %v", err)
	}
}

// full flow over the real queue + runner: api request -> worker -> discovery -> extraction.
func TestCrawlSiteEndToEnd(t *testing.T) {
	srv := shop(t)
	e := newEnv(t, srv.URL)
	ctx := context.Background()
	run, _, err := e.svc.Request(ctx, crawl.RunCrawlSite, e.site.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &jobs.Runner{Queue: e.q, Worker: "t", PollMin: time.Millisecond, PollMax: 10 * time.Millisecond}
	r.Register(pipeline.JobRun, e.svc.Handle)
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = r.Run(rctx); close(done) }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		got, _ := e.runs.GetRun(ctx, run.ID)
		if got.Status == crawl.RunCompleted || got.Status == crawl.RunFailed || time.Now().After(deadline) {
			run = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if run.Status != crawl.RunCompleted || run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("run %+v", run)
	}
	disc, _ := run.Stats["discovery"].(map[string]any)
	ext, _ := run.Stats["extraction"].(map[string]any)
	if disc["new_urls"] != float64(5) || disc["pages"] != float64(2) || ext["succeeded"] != float64(4) || ext["failed"] != float64(1) {
		t.Fatalf("stats %+v", run.Stats)
	}
	st, _ := postgres.URLs{Pool: e.pool}.SiteStats(ctx, e.site.ID)
	if st.Completed != 4 || st.Failed != 1 || st.ExtractedItems != 4 {
		t.Fatalf("site stats %+v", st)
	}
}

type failingExtractor struct{ err error }

func (f failingExtractor) ProcessBatch(context.Context, int64) (extraction.Batch, error) {
	return extraction.Batch{}, f.err
}

func runHandle(t *testing.T, e *env, run crawl.Run, attempt, maxA int) error {
	t.Helper()
	return e.svc.Handle(context.Background(), jobs.Job{ID: run.JobID, Kind: pipeline.JobRun, Payload: []byte(fmt.Sprintf(`{"run_id":%d}`, run.ID)), Attempts: attempt, MaxAttempts: maxA})
}

func TestHandleRetryThenFail(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	e.svc.Extraction = failingExtractor{err: errors.New("disk full")}
	run, _, _ := e.svc.Request(ctx, crawl.RunExtractSite, e.site.ID, 0)
	if err := runHandle(t, e, run, 1, 3); err == nil {
		t.Fatal("want error")
	}
	got, _ := e.runs.GetRun(ctx, run.ID)
	if got.Status != crawl.RunQueued || got.Error != "disk full" || got.StartedAt == nil {
		t.Fatalf("retryable: %+v", got)
	}
	if err := runHandle(t, e, run, 3, 3); err == nil {
		t.Fatal("want error")
	}
	got, _ = e.runs.GetRun(ctx, run.ID)
	if got.Status != crawl.RunFailed || got.FinishedAt == nil {
		t.Fatalf("last attempt: %+v", got)
	}
	// finished run: handler no-op.
	if err := runHandle(t, e, run, 1, 3); err != nil {
		t.Fatalf("terminal rerun: %v", err)
	}
}

func TestHandleDeletedSiteIsNoop(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	run, _, _ := e.svc.Request(ctx, crawl.RunExtractSite, e.site.ID, 0)
	if err := (postgres.Sites{Pool: e.pool}).DeleteSite(ctx, e.site.ID); err != nil {
		t.Fatal(err)
	}
	if err := runHandle(t, e, run, 1, 3); err != nil {
		t.Fatalf("deleted site: %v", err)
	}
	if err := e.svc.Handle(ctx, jobs.Job{Payload: []byte(`{}`)}); !jobs.IsPermanent(err) {
		t.Fatalf("bad payload: %v", err)
	}
}

// worker killed on last attempt: next claim fails job AND its run (never stuck RUNNING).
func TestCrashGuardFailsRun(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	run, _, _ := e.svc.Request(ctx, crawl.RunExtractSite, e.site.ID, 0)
	if _, err := e.pool.Exec(ctx, `UPDATE jobs SET status='running', attempts=max_attempts, locked_by='dead', locked_until=now()-interval '1 minute' WHERE id=$1`, run.JobID); err != nil {
		t.Fatal(err)
	}
	_ = e.runs.StartRun(ctx, run.ID)
	if got, err := e.q.Claim(ctx, "w", 5, time.Minute); err != nil || len(got) != 0 {
		t.Fatalf("claim: %v %+v", err, got)
	}
	got, _ := e.runs.GetRun(ctx, run.ID)
	if got.Status != crawl.RunFailed || got.Error == "" {
		t.Fatalf("run %+v", got)
	}
}

type panicExtractor struct{}

func (panicExtractor) ProcessBatch(context.Context, int64) (extraction.Batch, error) { panic("boom") }

func TestPanicOnLastAttemptFailsRun(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	e.svc.Extraction = panicExtractor{}
	run, _, _ := e.svc.Request(ctx, crawl.RunExtractSite, e.site.ID, 0)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic swallowed")
			}
		}()
		_ = runHandle(t, e, run, 3, 3)
	}()
	got, _ := e.runs.GetRun(ctx, run.ID)
	if got.Status != crawl.RunFailed || got.Error != "panic: boom" {
		t.Fatalf("run %+v", got)
	}
}

type blockingExtractor struct{}

func (blockingExtractor) ProcessBatch(ctx context.Context, _ int64) (extraction.Batch, error) {
	<-ctx.Done()
	return extraction.Batch{}, ctx.Err()
}

// lease lost: job belongs to someone else (or crash guard failed it) -> run status untouched.
func TestLeaseLostLeavesRunAlone(t *testing.T) {
	e := newEnv(t, "https://x.pe")
	ctx := context.Background()
	e.svc.Extraction = blockingExtractor{}
	run, _, _ := e.svc.Request(ctx, crawl.RunExtractSite, e.site.ID, 0)
	jctx, cancel := context.WithCancelCause(ctx)
	errc := make(chan error, 1)
	go func() {
		errc <- e.svc.Handle(jctx, jobs.Job{Payload: []byte(fmt.Sprintf(`{"run_id":%d}`, run.ID)), Attempts: 1, MaxAttempts: 3})
	}()
	for {
		got, _ := e.runs.GetRun(ctx, run.ID)
		if got.Status == crawl.RunRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// crash guard path: run marked FAILED by another process while we still run.
	_ = e.runs.FinishRun(ctx, run.ID, crawl.RunFailed, nil, "worker died repeatedly")
	cancel(jobs.ErrLeaseLost)
	if err := <-errc; err == nil {
		t.Fatal("want ctx error")
	}
	got, _ := e.runs.GetRun(ctx, run.ID)
	if got.Status != crawl.RunFailed || got.Error != "worker died repeatedly" {
		t.Fatalf("run overwritten: %+v", got)
	}
}
