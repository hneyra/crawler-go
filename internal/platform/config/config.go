// Package config loads runtime settings from env. one place. validated at start.
// every bad key reported at once -> fix env in one go.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config = all runtime settings.
type Config struct {
	Env         string // dev | test | prod
	HTTPAddr    string
	MetricsAddr string
	DatabaseURL string
	// DBMaxConns = pgx pool size per process. 1..1000.
	DBMaxConns int
	// PublicURL = origin seen by browsers (web ui). CSRF origin check.
	PublicURL      string
	TrustedProxies []netip.Prefix // xff trusted only from these

	SessionTTL              time.Duration
	CookieSecure            bool
	PasswordHashConcurrency int
	RateLimit               bool // per ip/user limiters. off only outside prod.

	WorkerConcurrency int
	WorkerHealthAddr  string

	Crawl Crawl
}

// Crawl = crawler tuning. defaults = java crawler (crawler.* properties).
type Crawl struct {
	RawDataDir          string        // snapshots root. {dir}/{site}/{date}/{sha}.html
	DiscoveryMaxPages   int           // listing pages per category run
	ExtractionBatchSize int           // urls claimed per batch
	RetryMax            int           // FAILED urls retried while retry_count < max
	RetryInterval       time.Duration // retry sweep period
	StuckAfter          time.Duration // IN_PROGRESS older than this -> back to PENDING
	FetchTimeout        time.Duration // plain http fetch
	RenderTimeout       time.Duration // headless render
	UserAgent           string
	ChromePath          string // empty -> chromedp finds chrome on PATH
	BrowserProfileDir   string // persistent profile: cloudflare cookies survive
	Headless            bool
}

// Load reads env via getenv.
func Load(getenv func(string) string) (Config, error) {
	r := reader{get: getenv}
	c := Config{
		Env:                     r.str("CRAWLER_ENV", "dev"),
		HTTPAddr:                r.str("CRAWLER_HTTP_ADDR", ":8080"),
		MetricsAddr:             r.str("CRAWLER_METRICS_ADDR", "127.0.0.1:9091"),
		DatabaseURL:             r.str("CRAWLER_DATABASE_URL", ""),
		DBMaxConns:              r.int("CRAWLER_DB_MAX_CONNS", max(4, runtime.NumCPU())),
		PublicURL:               strings.TrimRight(r.str("CRAWLER_PUBLIC_URL", "http://localhost:5173"), "/"),
		SessionTTL:              r.dur("CRAWLER_SESSION_TTL", 7*24*time.Hour),
		PasswordHashConcurrency: r.int("CRAWLER_PASSWORD_HASH_CONCURRENCY", 4),
		WorkerConcurrency:       r.int("CRAWLER_WORKER_CONCURRENCY", 2),
		WorkerHealthAddr:        r.str("CRAWLER_WORKER_HEALTH_ADDR", "127.0.0.1:8081"),
		Crawl: Crawl{
			RawDataDir:          r.str("CRAWLER_RAW_DATA_DIR", "./data/raw"),
			DiscoveryMaxPages:   r.int("CRAWLER_DISCOVERY_MAX_PAGES", 50),
			ExtractionBatchSize: r.int("CRAWLER_EXTRACTION_BATCH_SIZE", 10),
			RetryMax:            r.int("CRAWLER_RETRY_MAX", 3),
			RetryInterval:       r.dur("CRAWLER_RETRY_INTERVAL", 10*time.Minute),
			StuckAfter:          r.dur("CRAWLER_STUCK_AFTER", 30*time.Minute),
			FetchTimeout:        r.dur("CRAWLER_FETCH_TIMEOUT", 15*time.Second),
			RenderTimeout:       r.dur("CRAWLER_RENDER_TIMEOUT", 30*time.Second),
			UserAgent: r.str("CRAWLER_USER_AGENT",
				"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
			ChromePath:        r.str("CRAWLER_CHROME_PATH", ""),
			BrowserProfileDir: r.str("CRAWLER_BROWSER_PROFILE_DIR", ""),
			Headless:          r.bool("CRAWLER_HEADLESS", true),
		},
	}
	c.CookieSecure = r.bool("CRAWLER_COOKIE_SECURE", c.Env == "prod")
	switch rl := r.str("CRAWLER_RATE_LIMIT", "on"); rl {
	case "on":
		c.RateLimit = true
	case "off":
	default:
		r.fail("CRAWLER_RATE_LIMIT must be on|off, got %q", rl)
		c.RateLimit = true
	}
	c.TrustedProxies = parsePrefixes(&r, r.str("CRAWLER_TRUSTED_PROXIES", ""))
	c.validate(&r)
	if len(r.errs) > 0 {
		return Config{}, errors.Join(r.errs...)
	}
	return c, nil
}

// FromEnv = Load(os.Getenv).
func FromEnv() (Config, error) { return Load(os.Getenv) }

func (c *Config) validate(r *reader) {
	if c.DatabaseURL == "" {
		r.fail("CRAWLER_DATABASE_URL required")
	}
	// unknown env (e.g. "production") would skip prod guards. refuse.
	if c.Env != "dev" && c.Env != "test" && c.Env != "prod" {
		r.fail("CRAWLER_ENV must be dev|test|prod, got %q", c.Env)
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > 1000 {
		r.fail("CRAWLER_DB_MAX_CONNS must be 1..1000")
	}
	for k, v := range map[string]string{"CRAWLER_HTTP_ADDR": c.HTTPAddr, "CRAWLER_METRICS_ADDR": c.MetricsAddr, "CRAWLER_WORKER_HEALTH_ADDR": c.WorkerHealthAddr} {
		if !validListenAddr(v) {
			r.fail("%s: bad listen addr %q", k, v)
		}
	}
	if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		r.fail("CRAWLER_PUBLIC_URL must be an origin like https://crawler.example, got %q", c.PublicURL)
	}
	if c.Env == "prod" && !c.RateLimit {
		r.fail("CRAWLER_RATE_LIMIT=off refused in prod")
	}
	if c.PasswordHashConcurrency < 1 || c.PasswordHashConcurrency > 64 {
		r.fail("CRAWLER_PASSWORD_HASH_CONCURRENCY must be 1..64")
	}
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 64 {
		r.fail("CRAWLER_WORKER_CONCURRENCY must be 1..64")
	}
	cr := c.Crawl
	if cr.RawDataDir == "" {
		r.fail("CRAWLER_RAW_DATA_DIR required")
	}
	if cr.DiscoveryMaxPages < 1 || cr.DiscoveryMaxPages > 10000 {
		r.fail("CRAWLER_DISCOVERY_MAX_PAGES must be 1..10000")
	}
	if cr.ExtractionBatchSize < 1 || cr.ExtractionBatchSize > 1000 {
		r.fail("CRAWLER_EXTRACTION_BATCH_SIZE must be 1..1000")
	}
	if cr.RetryMax < 0 || cr.RetryMax > 100 {
		r.fail("CRAWLER_RETRY_MAX must be 0..100")
	}
}

// validListenAddr: host:port, numeric port 1..65535. empty host = all interfaces.
func validListenAddr(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil || strings.ContainsAny(host, " /") {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

type reader struct {
	get  func(string) string
	errs []error
}

func (r *reader) fail(f string, a ...any) { r.errs = append(r.errs, fmt.Errorf(f, a...)) }

func (r *reader) str(k, def string) string {
	if v := strings.TrimSpace(r.get(k)); v != "" {
		return v
	}
	return def
}

func (r *reader) int(k string, def int) int {
	v := strings.TrimSpace(r.get(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.fail("%s: not int: %q", k, v)
		return def
	}
	return n
}

func (r *reader) bool(k string, def bool) bool {
	v := strings.TrimSpace(r.get(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.fail("%s: not bool: %q", k, v)
		return def
	}
	return b
}

func (r *reader) dur(k string, def time.Duration) time.Duration {
	v := strings.TrimSpace(r.get(k))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.fail("%s: bad duration: %q", k, v)
		return def
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePrefixes(r *reader, s string) []netip.Prefix {
	var out []netip.Prefix
	for _, part := range splitList(s) {
		p, err := netip.ParsePrefix(part)
		if err != nil {
			r.fail("CRAWLER_TRUSTED_PROXIES: bad cidr %q", part)
			continue
		}
		out = append(out, p)
	}
	return out
}
