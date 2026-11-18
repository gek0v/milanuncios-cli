package milanuncios_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func TestDetectImageMime_StrictMagicBytes(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Text file pretending to be JPEG via extension
	fakeJpeg := filepath.Join(tmpDir, "secret.jpg")
	if err := os.WriteFile(fakeJpeg, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"), 0644); err != nil {
		t.Fatalf("failed to write fake jpeg: %v", err)
	}

	client := milanuncios.NewClient("test")
	client.SetSession("fake-token")

	_, err := client.UploadPhoto(context.Background(), fakeJpeg, 0)
	if err == nil {
		t.Fatalf("expected UploadPhoto to reject text file with fake .jpg extension")
	}
	if !strings.Contains(err.Error(), "unsupported local image format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}

	// 2. Text file pretending to be PNG via extension
	fakePng := filepath.Join(tmpDir, "passwords.png")
	if err := os.WriteFile(fakePng, []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644); err != nil {
		t.Fatalf("failed to write fake png: %v", err)
	}

	_, err = client.UploadPhoto(context.Background(), fakePng, 0)
	if err == nil {
		t.Fatalf("expected UploadPhoto to reject text file with fake .png extension")
	}
	if !strings.Contains(err.Error(), "unsupported local image format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}

	// 3. Fake WebP
	fakeWebp := filepath.Join(tmpDir, "notes.webp")
	if err := os.WriteFile(fakeWebp, []byte("Some secret notes here"), 0644); err != nil {
		t.Fatalf("failed to write fake webp: %v", err)
	}

	_, err = client.UploadPhoto(context.Background(), fakeWebp, 0)
	if err == nil {
		t.Fatalf("expected UploadPhoto to reject text file with fake .webp extension")
	}
	if !strings.Contains(err.Error(), "unsupported local image format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}
}

func TestUploadPhoto_SSRFProtection(t *testing.T) {
	client := milanuncios.NewClient("test")
	client.SetSession("fake-token")
	// Note: client.AllowPrivateIPs is false by default

	blockedTargets := []struct {
		name string
		url  string
	}{
		{"IPv4 Loopback", "http://127.0.0.1:8080/photo.jpg"},
		{"Localhost name", "http://localhost:8080/photo.jpg"},
		{"AWS Metadata IPv4", "http://169.254.169.254/latest/meta-data/"},
		{"Cloud Link-Local", "http://169.254.1.1/image.png"},
		{"RFC1918 Class A", "http://10.0.0.1/private.jpg"},
		{"RFC1918 Class B", "http://172.16.0.1/internal.png"},
		{"RFC1918 Class C", "http://192.168.1.1/router.jpg"},
		{"IPv6 Loopback", "http://[::1]:8080/photo.jpg"},
	}

	for _, tc := range blockedTargets {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.UploadPhoto(context.Background(), tc.url, 0)
			if err == nil {
				t.Fatalf("expected SSRF protection to block %s (%s)", tc.name, tc.url)
			}
			if !strings.Contains(err.Error(), "anti-SSRF policy") && !strings.Contains(err.Error(), "blocked") {
				t.Errorf("expected SSRF blocked message, got: %v", err)
			}
		})
	}
}

func TestUploadPhoto_SSRFRedirectBlocked(t *testing.T) {
	// Server that attempts to redirect to AWS metadata 169.254.169.254
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirectServer.Close()

	client := milanuncios.NewClient("test")
	client.SetSession("fake-token")
	// Allow accessing the initial redirect server (since httptest binds loopback)
	// but the redirect to 169.254.169.254 should still be caught!
	t.Setenv("MILANUNCIOS_ALLOW_PRIVATE_IPS", "")

	// Direct call to redirectServer without AllowPrivateIPs is blocked at initial step:
	_, err := client.UploadPhoto(context.Background(), redirectServer.URL+"/redirect.jpg", 0)
	if err == nil {
		t.Fatalf("expected initial connection to loopback to be blocked by SSRF policy")
	}
}
