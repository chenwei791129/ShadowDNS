package doh

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

// acmeChallengeBasePath is the RFC 8555 HTTP-01 challenge path prefix.
const acmeChallengeBasePath = "/.well-known/acme-challenge/"

// acmeProfile is the Let's Encrypt certificate profile required for IP-address
// certificates: a short-lived (~6 day) profile. Fixed because this change
// targets the IP-certificate scenario exclusively (see design Non-Goals).
const acmeProfile = "shortlived"

// renewRetryInterval is how long the renewal loop waits before retrying after
// a failed obtain/renew. Small relative to the ~6 day certificate lifetime so
// many retries fit inside the renewal lead time before the current cert
// expires.
const renewRetryInterval = 10 * time.Minute

// renewLeadFraction is the fraction of total certificate lifetime before
// expiry at which renewal begins (1/3 → renew ~2 days before a 6 day cert
// expires), per design.
const renewLeadFraction = 3

// minRenewInterval is the minimum delay between successful obtain/renew
// attempts. It floors the renewal loop so a certificate already inside its
// renewal lead window (or any case where renewDelay computes 0) cannot spin
// the loop and hammer the ACME directory into rate-limiting the account.
const minRenewInterval = time.Minute

// certMetrics records certificate renewal outcomes and expiry. Implemented by
// *metrics.Metrics (task 6.2); a nil value disables recording.
type certMetrics interface {
	RecordDoHCertRenewal(result string) // "success" | "failure"
	SetDoHCertNotAfter(t time.Time)
}

// challengeResponder is a long-lived ACME HTTP-01 responder. It implements
// lego's challenge.Provider (Present/CleanUp store and drop token→keyAuth
// pairs) and serves those key authorizations on a dedicated listener that
// returns 404 for every path outside /.well-known/acme-challenge/. Keeping the
// listener long-lived (rather than letting lego bring one up per solve) lets
// the server own its timeouts and graceful shutdown and keeps port 80 bound
// for the unpredictable multi-perspective Let's Encrypt validation.
type challengeResponder struct {
	tokens sync.Map // token(string) -> keyAuth(string)
	logger *zap.Logger
}

var _ challenge.Provider = (*challengeResponder)(nil)

func newChallengeResponder(logger *zap.Logger) *challengeResponder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &challengeResponder{logger: logger}
}

// Present stores the key authorization so the listener can serve it.
func (c *challengeResponder) Present(_, token, keyAuth string) error {
	c.tokens.Store(token, keyAuth)
	return nil
}

// CleanUp drops the key authorization once the challenge is validated.
func (c *challengeResponder) CleanUp(_, token, _ string) error {
	c.tokens.Delete(token)
	return nil
}

// Handler serves the stored key authorizations. Only paths under
// /.well-known/acme-challenge/ with a known token return 200 + the key
// authorization; everything else (including an empty token) returns 404.
func (c *challengeResponder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(acmeChallengeBasePath, func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, acmeChallengeBasePath)
		v, ok := c.tokens.Load(token)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, v.(string))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return mux
}

// acmeUser implements lego's registration.User for a single ACME account whose
// key is persisted to disk (loadOrCreateAccountKey) and reused across process
// restarts and registration retries, so re-registration with the same key is
// idempotent and does not mint a new ACME account.
type acmeUser struct {
	key crypto.PrivateKey
	reg *registration.Resource
}

// GetEmail returns an empty string: the ACME account is registered without a
// contact. lego's Registrar.Register sends an empty Contact when GetEmail is
// empty, which Let's Encrypt accepts (RFC 8555 §7.3 contact is optional). The
// method exists only to satisfy lego's registration.User interface.
func (u *acmeUser) GetEmail() string                        { return "" }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// newLegoObtainer builds an ACME obtain function bound to a prepared,
// registered lego client. The returned function obtains a fresh certificate
// for the configured IP via HTTP-01 + the shortlived profile each time it is
// called. The HTTP-01 challenges are answered by responder, so the caller MUST
// have responder's listener running on the configured http01_listen.
func newLegoObtainer(cfg shadowdnscfg.DoHACMEConfig, responder challenge.Provider) (func(context.Context) (*tls.Certificate, error), error) {
	accountKey, err := loadOrCreateAccountKey(cfg.AccountKeyFile)
	if err != nil {
		return nil, err
	}
	user := &acmeUser{key: accountKey}

	legoCfg := lego.NewConfig(user)
	legoCfg.CADirURL = cfg.DirectoryURL
	legoCfg.Certificate.KeyType = "P256"

	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, fmt.Errorf("doh acme: new client: %w", err)
	}
	if err := client.Challenge.SetHTTP01Provider(responder); err != nil {
		return nil, fmt.Errorf("doh acme: set http-01 provider: %w", err)
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("doh acme: register account: %w", err)
	}
	user.reg = reg

	ipStr := cfg.IP.String()
	obtain := func(_ context.Context) (*tls.Certificate, error) {
		// Build the CSR ourselves with the IP in the SubjectAltName only and an
		// empty Common Name. lego's Obtain(Domains) path copies Domains[0] into
		// the CN, and Let's Encrypt rejects a CSR carrying an IP address in the
		// CN (badCSR at finalize). ObtainForCSR derives the IP identifier from
		// the SAN, so the order is still an RFC 8738 IP order.
		csr, certKey, err := buildIPCSR(cfg.IP)
		if err != nil {
			return nil, err
		}
		res, err := client.Certificate.ObtainForCSR(certificate.ObtainForCSRRequest{
			CSR:        csr,
			PrivateKey: certKey,
			Bundle:     true,
			Profile:    acmeProfile,
		})
		if err != nil {
			return nil, fmt.Errorf("doh acme: obtain certificate for %s: %w", ipStr, err)
		}
		cert, err := tls.X509KeyPair(res.Certificate, res.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("doh acme: parse issued key pair: %w", err)
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("doh acme: parse issued leaf: %w", err)
		}
		cert.Leaf = leaf
		return &cert, nil
	}
	return obtain, nil
}

// buildIPCSR generates a certificate key and a CSR for an RFC 8738 IP-address
// certificate. The IP is placed in the SubjectAltName iPAddress field only; the
// Subject (and therefore the Common Name) is left empty, because Let's Encrypt
// rejects with badCSR any finalize whose CSR carries an IP address in the CN.
// pebble does not enforce this, so it must be guarded by a unit test rather
// than the pebble integration test.
func buildIPCSR(ip netip.Addr) (*x509.CertificateRequest, crypto.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("doh acme: generate certificate key: %w", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		IPAddresses: []net.IP{net.IP(ip.AsSlice())},
	}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("doh acme: create CSR: %w", err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, nil, fmt.Errorf("doh acme: parse CSR: %w", err)
	}
	return csr, key, nil
}

// newLazyLegoObtainer returns an obtain function that builds and registers the
// lego client on first use (and caches it), so a transient ACME-directory
// outage at startup is retried by the renewal loop instead of permanently
// disabling DoH until a process restart.
func newLazyLegoObtainer(cfg shadowdnscfg.DoHACMEConfig, responder challenge.Provider) func(context.Context) (*tls.Certificate, error) {
	var (
		mu     sync.Mutex
		cached func(context.Context) (*tls.Certificate, error)
	)
	return func(ctx context.Context) (*tls.Certificate, error) {
		mu.Lock()
		if cached == nil {
			o, err := newLegoObtainer(cfg, responder)
			if err != nil {
				mu.Unlock()
				return nil, err
			}
			cached = o
		}
		o := cached
		mu.Unlock()
		return o(ctx)
	}
}

// certManager holds the live TLS certificate behind an atomic pointer and
// renews it in the background. GetCertificate (wired into tls.Config) reads the
// pointer per handshake, so a renewal swap takes effect on the next handshake
// without restarting the listener. A failed renewal keeps the current
// certificate and is recorded via metrics/log.
type certManager struct {
	cert          atomic.Pointer[tls.Certificate]
	obtain        func(context.Context) (*tls.Certificate, error)
	logger        *zap.Logger
	metrics       certMetrics
	retryInterval time.Duration
}

func newCertManager(obtain func(context.Context) (*tls.Certificate, error), m certMetrics, logger *zap.Logger) *certManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &certManager{
		obtain:        obtain,
		logger:        logger,
		metrics:       m,
		retryInterval: renewRetryInterval,
	}
}

// GetCertificate returns the current certificate for a TLS handshake, or an
// error when none has been obtained yet (handshakes fail until the first
// obtain succeeds, rather than serving a bogus certificate).
func (cm *certManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c := cm.cert.Load()
	if c == nil {
		return nil, errors.New("doh: no TLS certificate available yet")
	}
	return c, nil
}

// obtainAndStore obtains a fresh certificate and, on success, atomically
// replaces the current one and records the new expiry. On failure the current
// certificate is left in place and the failure is recorded; the error is
// returned so the renewal loop can schedule a retry. The installed certificate
// is returned on success (nil on failure) so the caller can schedule renewal
// off the cert it just installed without re-loading the atomic pointer.
func (cm *certManager) obtainAndStore(ctx context.Context) (*tls.Certificate, error) {
	cert, err := cm.obtain(ctx)
	if err != nil {
		cm.recordRenewal("failure")
		cm.logger.Sugar().Errorw("doh: certificate obtain/renew failed; keeping current certificate", "err", err)
		return nil, err
	}
	cm.cert.Store(cert)
	cm.recordRenewal("success")
	if cm.metrics != nil && cert.Leaf != nil {
		cm.metrics.SetDoHCertNotAfter(cert.Leaf.NotAfter)
	}
	cm.logger.Sugar().Infow("doh: certificate installed", "not_after", certNotAfter(cert))
	return cert, nil
}

func (cm *certManager) recordRenewal(result string) {
	if cm.metrics != nil {
		cm.metrics.RecordDoHCertRenewal(result)
	}
}

// run obtains the initial certificate (retrying on failure) and then renews it
// before expiry until ctx is cancelled. It returns when ctx is done. The first
// successful obtain unblocks TLS handshakes; before that GetCertificate errors.
func (cm *certManager) run(ctx context.Context) {
	for {
		// Bail before attempting an obtain if ctx is already cancelled (a fast
		// start/stop race): otherwise the first loop entry records a spurious
		// "failure" renewal metric and logs an ACME error for a shutdown that
		// never actually tried to issue.
		if ctx.Err() != nil {
			return
		}
		cert, err := cm.obtainAndStore(ctx)
		var wait time.Duration
		if err != nil {
			wait = cm.retryInterval
		} else {
			// Floor the success-path wait so a certificate that is already
			// inside its renewal lead window (clock skew, or renewDelay == 0)
			// cannot spin the obtain loop back-to-back and trip the ACME
			// directory's rate limits.
			wait = max(renewDelay(cert, time.Now()), minRenewInterval)
		}
		// time.NewTimer (not time.After) so the timer is stopped when ctx is
		// cancelled: the success-path wait can be multiple days, and a bare
		// time.After would leave that runtime timer on the heap until it fired
		// long after the loop has exited.
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// renewDelay returns how long to wait before renewing cert, given now. Renewal
// begins once the certificate is within 1/renewLeadFraction of its total
// lifetime from expiry. A nil leaf or already-past lead time yields 0 (renew
// immediately).
func renewDelay(cert *tls.Certificate, now time.Time) time.Duration {
	if cert == nil || cert.Leaf == nil {
		return 0
	}
	lifetime := cert.Leaf.NotAfter.Sub(cert.Leaf.NotBefore)
	renewAt := cert.Leaf.NotAfter.Add(-lifetime / renewLeadFraction)
	if d := renewAt.Sub(now); d > 0 {
		return d
	}
	return 0
}

func certNotAfter(cert *tls.Certificate) time.Time {
	if cert == nil || cert.Leaf == nil {
		return time.Time{}
	}
	return cert.Leaf.NotAfter
}
