package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func guardedRequest(method, target, origin, contentType string) (*httptest.ResponseRecorder, bool) {
	called := false
	handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(method, target, strings.NewReader(`{"ok":true}`))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, called
}

func TestRequestGuardRejectsDNSRebindingHostForRead(t *testing.T) {
	called := false
	handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/workspace", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestRequestGuardAllowsLoopbackHosts(t *testing.T) {
	for _, target := range []string{"http://localhost:8080/api/health", "http://127.0.0.1:8080/api/health", "http://[::1]:8080/api/health"} {
		t.Run(target, func(t *testing.T) {
			called := false
			handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || !called {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
		})
	}
}

func TestMutationGuardRejectsCrossSiteBrowserRequest(t *testing.T) {
	response, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/projects", "https://evil.example", "text/plain")
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestMutationGuardAllowsSameOriginJSON(t *testing.T) {
	response, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/projects", "http://localhost:8080", "application/json; charset=utf-8")
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestMutationGuardUsesForwardedDashboardOrigin(t *testing.T) {
	called := false
	handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPost, "http://api:8080/api/projects", strings.NewReader(`{"ok":true}`))
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Host", "localhost:3000")
	request.Header.Set("X-Forwarded-Proto", "http")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestRequestGuardAllowsOnlyBundledExtensionOnBrowserSurface(t *testing.T) {
	bundledOrigin := "chrome-extension://" + bundledBrowserExtensionID
	allowed, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/browser/workers", bundledOrigin, "application/json")
	if allowed.Code != http.StatusNoContent || !called {
		t.Fatalf("allowed status=%d called=%v", allowed.Code, called)
	}
	deniedSurface, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/projects", bundledOrigin, "application/json")
	if deniedSurface.Code != http.StatusForbidden || called {
		t.Fatalf("denied surface status=%d called=%v", deniedSurface.Code, called)
	}
	deniedExtension, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/browser/workers", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "application/json")
	if deniedExtension.Code != http.StatusForbidden || called {
		t.Fatalf("denied extension status=%d called=%v", deniedExtension.Code, called)
	}
}

func TestRequestGuardRejectsOtherExtensionRead(t *testing.T) {
	called := false
	handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/browser/workers", nil)
	request.Header.Set("Origin", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestMutationGuardRejectsSimpleTextBodyEvenWithoutOrigin(t *testing.T) {
	response, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/projects", "", "text/plain")
	if response.Code != http.StatusUnsupportedMediaType || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestMutationGuardAllowsCLIWithoutOriginWhenJSON(t *testing.T) {
	response, called := guardedRequest(http.MethodPost, "http://localhost:8080/api/projects", "", "application/json")
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

func TestMutationGuardRejectsCrossSiteFetchMetadataWithoutOrigin(t *testing.T) {
	called := false
	handler := withMutationRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/projects/1/sync", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}
