package oauth

import "errors"

// Error is a refusal in OAuth's own terms (RFC 6749 §5.2 and §4.1.2.1):
// Code is what a client branches on, Description a line for whoever reads
// that client's logs. Both reach the client as they are, so neither ever
// carries a token, a hash or a database message — and Description stays
// ASCII, as the RFC requires.
type Error struct {
	Code        string
	Description string
}

func (e *Error) Error() string { return "oauth: " + e.Code + ": " + e.Description }

func invalidRequest(why string) *Error { return &Error{Code: "invalid_request", Description: why} }
func invalidGrant(why string) *Error   { return &Error{Code: "invalid_grant", Description: why} }
func invalidScope(why string) *Error   { return &Error{Code: "invalid_scope", Description: why} }
func invalidClient(why string) *Error  { return &Error{Code: "invalid_client", Description: why} }

// invalidTarget is RFC 8707's refusal of a resource this server does not
// issue tokens for.
func invalidTarget(why string) *Error { return &Error{Code: "invalid_target", Description: why} }

// ErrRequestNotFound covers every way an authorization request can be missing
// for the person answering it: never made, expired, already answered, opened
// first by someone else, or never opened by them. All mean the same to them —
// start again from Claude.
var ErrRequestNotFound = errors.New("oauth: authorization request not found")
