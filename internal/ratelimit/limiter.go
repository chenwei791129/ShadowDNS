package ratelimit

import (
	"net/netip"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// Action is the limiter's decision for a single UDP response.
type Action int

const (
	// Allow delivers the response unchanged.
	Allow Action = iota
	// Drop discards the response without writing it.
	Drop
	// Slip writes a TC=1 truncated response so a legitimate resolver retries
	// over TCP.
	Slip
)

// categoryAll is an internal pseudo-category used only as the account-key
// discriminator for the `all-per-second` aggregate gate. It is never produced
// by ClassifyResponse.
const categoryAll Category = numCategories

// accountKey identifies one credit account: a masked client address block, a
// response category (or categoryAll), and the imputed name. netip.Addr is
// comparable, so the whole struct is a valid map key with no allocation.
type accountKey struct {
	block    netip.Addr
	category Category
	name     string
}

// creditAccount holds a rolling-window credit balance for one account. The
// struct is fixed-size with no pointer fields so the account table can store it
// by value and reuse map slots cheaply.
type creditAccount struct {
	credit     float64   // current balance; negative means over-limit
	lastRefill time.Time // last time credit was regenerated; zero ⇒ fresh
	slip       uint32    // over-limit counter for the slip cadence (task 3.1)
}

// charge regenerates credit for the elapsed time (capped at ceiling), debits
// one credit for this response, and reports whether the response is over-limit
// (balance fell below zero). A fresh account starts full at the ceiling. The
// balance is floored at -ceiling so a sustained flood cannot drive recovery
// time unbounded.
func (a *creditAccount) charge(now time.Time, rate, ceiling float64) (overLimit bool) {
	if a.lastRefill.IsZero() {
		a.credit = ceiling
		a.lastRefill = now
	} else if elapsed := now.Sub(a.lastRefill).Seconds(); elapsed > 0 {
		a.credit += elapsed * rate
		if a.credit > ceiling {
			a.credit = ceiling
		}
		// Only advance lastRefill forward. A non-positive elapsed (a clock that
		// did not move, or an injected clock that stepped backward) leaves it
		// untouched so the next positive interval is not over-counted.
		a.lastRefill = now
	}
	a.credit--
	if a.credit < -ceiling {
		a.credit = -ceiling
	}
	return a.credit < 0
}

// Limiter is the BIND-compatible response rate limiter. A nil *Limiter is a
// valid no-op (every Decide returns Allow), which is how the server represents
// "rate limiting unconfigured".
type Limiter struct {
	rates    [numCategories]float64 // per-category credit rate; 0 ⇒ unlimited
	allRate  float64                // aggregate all-per-second rate; 0 ⇒ no gate
	window   float64                // rolling-window length in seconds
	slip     int
	v4Prefix int
	v6Prefix int
	logOnly  bool

	exempt  *exemptList
	table   *table
	metrics Recorder
	now     func() time.Time
}

// Option customizes a Limiter at construction.
type Option func(*Limiter)

// WithRecorder wires a metrics Recorder so the limiter reports its rate-limit
// actions (dropped, slipped, exempted, logonly_would_drop). A nil recorder
// leaves recording disabled.
func WithRecorder(r Recorder) Option {
	return func(l *Limiter) { l.metrics = r }
}

// SetRecorder attaches a metrics recorder after construction. It must be
// called before the limiter starts serving (no concurrent Decide), which
// the startup path guarantees. Nil-safe on a nil limiter.
func (l *Limiter) SetRecorder(r Recorder) {
	if l != nil {
		l.metrics = r
	}
}

// NewLimiter builds a Limiter from a parsed rate-limit configuration. It
// returns (nil, nil) when cfg is nil so callers can wire "unconfigured" through
// unchanged. It returns an error for configuration that ParseOptions did not
// already reject — currently a malformed exempt-clients entry.
func NewLimiter(cfg *config.RateLimitConfig, opts ...Option) (*Limiter, error) {
	if cfg == nil {
		return nil, nil //nolint:nilnil
	}
	exempt, err := newExemptList(cfg.ExemptClients)
	if err != nil {
		return nil, err
	}
	l := &Limiter{
		allRate:  float64(cfg.AllPerSecond),
		window:   float64(cfg.Window),
		slip:     cfg.Slip,
		v4Prefix: cfg.IPv4PrefixLength,
		v6Prefix: cfg.IPv6PrefixLength,
		logOnly:  cfg.LogOnly,
		exempt:   exempt,
		table:    newTable(cfg.MaxTableSize, cfg.MinTableSize),
		now:      time.Now,
	}
	l.rates[CategoryResponses] = float64(cfg.ResponsesPerSecond)
	l.rates[CategoryNodata] = float64(cfg.NodataPerSecond)
	l.rates[CategoryNxdomains] = float64(cfg.NxdomainsPerSecond)
	l.rates[CategoryErrors] = float64(cfg.ErrorsPerSecond)
	for _, opt := range opts {
		opt(l)
	}
	return l, nil
}

// Decide accounts one UDP response and returns the action to take. clientIP is
// the unmasked client address; category and name are derived by the caller from
// the response message. A response is over-limit when either its category
// account or the aggregate account goes negative; both accounts are always
// debited so neither budget leaks.
func (l *Limiter) Decide(clientIP netip.Addr, category Category, name string) Action {
	if l == nil {
		return Allow
	}
	// Exemption is checked before any account lookup so exempt traffic never
	// consumes credit or creates accounts.
	if l.exempt.contains(clientIP) {
		l.record(category, actionExempted)
		return Allow
	}

	block := l.maskAddr(clientIP)
	now := l.now()

	// Charge both the category account and the aggregate account; both are
	// always debited so neither budget leaks. Each charge returns whether that
	// account is over-limit and its slip cadence counter (advanced only when
	// over-limit). The slip count for the action is taken from the account that
	// actually triggered the limit, so an aggregate-only trigger paces on the
	// shared aggregate account rather than on a fresh per-name account.
	var (
		catOver, allOver bool
		catSlip, allSlip uint32
	)
	if rate := l.rateFor(category); rate > 0 {
		catOver, catSlip = l.table.charge(accountKey{block: block, category: category, name: name}, now, rate, l.window)
	}
	if l.allRate > 0 {
		allOver, allSlip = l.table.charge(accountKey{block: block, category: categoryAll}, now, l.allRate, l.window)
	}
	if !catOver && !allOver {
		return Allow
	}

	// Log-only: account as if enforcing, record the would-be action, but
	// deliver the response unchanged.
	if l.logOnly {
		l.record(category, actionLogonlyWouldDrop)
		return Allow
	}

	// Enforce: choose drop vs TC=1 truncation from the slip cadence of the
	// account that triggered the limit (category takes precedence when both).
	slip := allSlip
	if catOver {
		slip = catSlip
	}
	action := slipAction(l.slip, slip)
	if action == Slip {
		l.record(category, actionSlipped)
	} else {
		l.record(category, actionDropped)
	}
	return action
}

// record reports a rate-limit action to the metrics recorder when one is wired.
func (l *Limiter) record(category Category, action string) {
	if l.metrics != nil {
		l.metrics.RecordRateLimit(category.String(), action)
	}
}

// rateFor returns the per-second rate for a category, or 0 for categoryAll and
// any out-of-range value (those are handled separately / never limited here).
func (l *Limiter) rateFor(category Category) float64 {
	if int(category) < len(l.rates) {
		return l.rates[category]
	}
	return 0
}

// maskAddr reduces a client address to its accounting block by zeroing the host
// bits beyond the configured prefix length (ipv4-prefix-length for IPv4,
// ipv6-prefix-length otherwise). An invalid address is returned unchanged.
func (l *Limiter) maskAddr(ip netip.Addr) netip.Addr {
	if !ip.IsValid() {
		return ip
	}
	bits := l.v6Prefix
	if ip.Is4() {
		bits = l.v4Prefix
	}
	p, err := ip.Prefix(bits)
	if err != nil {
		return ip
	}
	return p.Addr()
}
