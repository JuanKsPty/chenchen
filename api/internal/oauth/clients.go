// Package oauth lets Claude sign in to chenchén: OAuth 2.1 with PKCE, where
// chenchén is both the authorization server that issues Claude's tokens and
// the resource — the MCP endpoint — those tokens are for (ADR-0004).
//
// Only two clients exist, both Claude's, named by their client ID metadata
// documents and fixed here; nothing is registered and nothing is fetched. The
// owner approves a connection from the app, in their own session, and can end
// it from there. What Claude receives — chno_ access tokens and chnr_ refresh
// tokens — lives in tables of its own, apart from the web's sessions: a
// session never opens the MCP endpoint, and nothing Claude holds ever opens
// an endpoint a session reaches.
package oauth

import (
	"net/url"
	"strconv"
	"strings"
)

// The client IDs Claude identifies itself with. Each is the URL of a client ID
// metadata document, which is what lets a server know a client without
// registering it; these two are known in advance, so neither is ever
// downloaded — a fetch would only add a network dependency and an edge that
// is known to refuse servers.
const (
	ClaudeClientID     = "https://claude.ai/oauth/mcp-oauth-client-metadata"
	ClaudeCodeClientID = "https://claude.ai/oauth/claude-code-client-metadata"
)

// client is one app allowed to ask for a token, and where it may be sent back.
type client struct {
	id string
	// name is what the consent page and Conexiones call it.
	name string
	// redirects reports whether uri is one of this client's own.
	redirects func(uri string) bool
}

var clients = map[string]client{
	ClaudeClientID:     {id: ClaudeClientID, name: "Claude", redirects: claudeCallback},
	ClaudeCodeClientID: {id: ClaudeCodeClientID, name: "Claude Code", redirects: loopbackCallback},
}

// lookupClient returns the client with this ID, if it is one of the two.
func lookupClient(id string) (client, bool) {
	c, ok := clients[id]
	return c, ok
}

// clientName is what to call a client the database names; an unknown one,
// which nothing should ever have stored, is just "Claude".
func clientName(id string) string {
	if c, ok := clients[id]; ok {
		return c.name
	}
	return "Claude"
}

// claudeCallback is claude.ai's callback, by exact string. Both domains are
// accepted: the app has been seen sending either.
func claudeCallback(uri string) bool {
	return uri == "https://claude.ai/api/mcp/auth_callback" || uri == "https://claude.com/api/mcp/auth_callback"
}

// loopbackCallback is Claude Code's: a listener it opens on the person's own
// machine for the length of the sign-in, on whatever port was free (RFC 8252
// §7.3). Everything but the port is fixed — plain http, one of the two
// loopback names, /callback, and nothing else at all.
func loopbackCallback(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" || u.Opaque != "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(uri, "#") {
		return false
	}
	if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" {
		return false
	}
	// url.Parse holds a port to digits but not to a range, and takes an
	// empty one after the colon for "no port".
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return false
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return false
	}
	return u.Path == "/callback" && u.RawPath == ""
}

// redirectHost is what the consent page shows of where the code will go: the
// host alone, since a loopback port means nothing to a person.
func redirectHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
