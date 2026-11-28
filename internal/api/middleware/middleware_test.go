package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/config"
)

func TestBasicAuth(t *testing.T) {
	cfg := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "s3cr3t-password",
	}

	e := echo.New()
	e.Use(BasicAuth(cfg))
	e.GET("/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/metrics", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/api/v1/dashboard", func(c echo.Context) error { return c.String(http.StatusOK, "secret") })

	tests := []struct {
		name       string
		path       string
		user, pass string
		setAuth    bool
		wantStatus int
	}{
		{"health skips auth", "/health", "", "", false, http.StatusOK},
		{"metrics skips auth", "/metrics", "", "", false, http.StatusOK},
		{"missing credentials", "/api/v1/dashboard", "", "", false, http.StatusUnauthorized},
		{"wrong password", "/api/v1/dashboard", "admin", "wrong", true, http.StatusUnauthorized},
		{"wrong username", "/api/v1/dashboard", "other", "s3cr3t-password", true, http.StatusUnauthorized},
		{"different length password", "/api/v1/dashboard", "admin", "x", true, http.StatusUnauthorized},
		{"valid credentials", "/api/v1/dashboard", "admin", "s3cr3t-password", true, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.setAuth {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestRequireAuth(t *testing.T) {
	cfg := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "s3cr3t-password",
	}

	e := echo.New()
	e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, RequireAuth(cfg))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing creds status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.SetBasicAuth("admin", "s3cr3t-password")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid creds status = %d, want 200", rec.Code)
	}
}

func TestBasicAuthCustomMetricsPath(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Username: "admin", Password: "secret"}
	e := echo.New()
	e.Use(BasicAuth(cfg, "/internal/metrics"))
	e.GET("/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/internal/metrics", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/metrics", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/health", http.StatusOK},
		{"/internal/metrics", http.StatusOK},
		{"/metrics", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("GET %s status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestCrossOriginProtection(t *testing.T) {
	e := echo.New()
	e.Use(CrossOriginProtection())
	e.GET("/api/v1/dashboard", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.POST("/api/v1/snapshots", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	tests := []struct {
		name       string
		method     string
		path       string
		headers    map[string]string
		wantStatus int
	}{
		{
			name:       "cross-site POST via Sec-Fetch-Site blocked",
			method:     http.MethodPost,
			path:       "/api/v1/snapshots",
			headers:    map[string]string{"Sec-Fetch-Site": "cross-site"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "cross-origin POST via Origin blocked",
			method:     http.MethodPost,
			path:       "/api/v1/snapshots",
			headers:    map[string]string{"Origin": "https://evil.example"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "same-origin POST via Sec-Fetch-Site allowed",
			method:     http.MethodPost,
			path:       "/api/v1/snapshots",
			headers:    map[string]string{"Sec-Fetch-Site": "same-origin"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "same-origin POST via matching Origin allowed",
			method:     http.MethodPost,
			path:       "/api/v1/snapshots",
			headers:    map[string]string{"Origin": "http://example.com"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "non-browser POST without Origin/Sec-Fetch-Site allowed",
			method:     http.MethodPost,
			path:       "/api/v1/snapshots",
			wantStatus: http.StatusOK,
		},
		{
			name:       "cross-site GET allowed",
			method:     http.MethodGet,
			path:       "/api/v1/dashboard",
			headers:    map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://example.com"+tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
