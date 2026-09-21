// Package grafanaproxy embeds Grafana under /grafana on the platform
// domain: same-origin iframes need no second login because every proxied
// request carries the platform's Grafana service credentials server-side.
//
// Grafana runs with serve_from_sub_path=true, so it natively serves the
// /grafana/... path space and emits /grafana-prefixed links itself — the
// proxy must NOT strip the prefix, or Grafana's own redirects (login,
// dashboard links) would jump the iframe outside /grafana.
package grafanaproxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// Options configures the proxy target and credentials.
type Options struct {
	// Target is the Grafana base URL, e.g. http://grafana.platform.svc
	Target string
	// Token is a Grafana service-account token (preferred).
	Token string
	// User/Password fall back to Grafana basic auth.
	User     string
	Password string
}

// Prefix is the mount path on the platform domain.
const Prefix = "/grafana"

// Handler builds the gin handler forwarding /grafana/* verbatim and
// injecting the service credentials.
func Handler(opts Options) (gin.HandlerFunc, error) {
	target, err := url.Parse(strings.TrimSuffix(opts.Target, "/"))
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	defaultDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		defaultDirector(req)
		req.Host = target.Host
		switch {
		case opts.Token != "":
			req.Header.Set("Authorization", "Bearer "+opts.Token)
		case opts.User != "":
			req.SetBasicAuth(opts.User, opts.Password)
		}
	}
	return func(c *gin.Context) {
		// /grafana (no slash) keeps relative links working after the jump.
		if c.Request.URL.Path == Prefix {
			c.Redirect(http.StatusMovedPermanently, Prefix+"/")
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}
