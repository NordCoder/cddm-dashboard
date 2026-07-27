package main

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const bundledBrowserExtensionID = "biakfbpkfdpniphmoafgldedkbnjfibp"

func withMutationRequestGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedLoopbackRequestHost(r) {
			http.Error(w, "request host is not a loopback authority", http.StatusMisdirectedRequest)
			return
		}
		if isExtensionRequest(r) && !allowedBundledExtensionRequest(r) {
			http.Error(w, "browser extension origin is not allowed", http.StatusForbidden)
			return
		}
		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if !allowedMutationOrigin(r) {
			http.Error(w, "cross-origin mutation is not allowed", http.StatusForbidden)
			return
		}
		if hasRequestBody(r) && !isJSONContentType(r.Header.Get("Content-Type")) {
			http.Error(w, "request body must use application/json", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func hasRequestBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) > 0
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
}

func forwardedAuthority(r *http.Request) (scheme, host string) {
	host = strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	scheme = strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme, host
}

func allowedLoopbackRequestHost(r *http.Request) bool {
	_, authority := forwardedAuthority(r)
	if authority == "" || strings.Contains(authority, ",") {
		return false
	}
	host := authority
	if parsed, _, err := net.SplitHostPort(authority); err == nil {
		host = parsed
	} else if strings.HasPrefix(authority, "[") && strings.HasSuffix(authority, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parsedOrigin(r *http.Request) (*url.URL, bool) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return nil, false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, true
	}
	return parsed, true
}

func isExtensionRequest(r *http.Request) bool {
	parsed, present := parsedOrigin(r)
	return present && parsed != nil && parsed.Scheme == "chrome-extension"
}

func allowedBundledExtensionRequest(r *http.Request) bool {
	parsed, present := parsedOrigin(r)
	return present && parsed != nil && parsed.Scheme == "chrome-extension" && parsed.Host == bundledBrowserExtensionID && strings.HasPrefix(r.URL.Path, "/api/browser/")
}

func allowedMutationOrigin(r *http.Request) bool {
	parsed, present := parsedOrigin(r)
	if !present {
		return !strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site")
	}
	if parsed == nil {
		return false
	}
	if parsed.Scheme == "chrome-extension" {
		return allowedBundledExtensionRequest(r)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	expectedProto, expectedHost := forwardedAuthority(r)
	return strings.EqualFold(parsed.Scheme, expectedProto) && strings.EqualFold(parsed.Host, expectedHost)
}
