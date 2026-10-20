package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// minimalPNG is a PNG signature followed by filler. screenshotPNG checks the
// signature only; the vision server decodes the image itself.
func minimalPNG() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("IMAGEDATA")...)
}

// A client names a file in screenshot_path. The server must never open it: the
// screenshot may come from another machine, and a path is an arbitrary read.
func TestScreenshotPNGNeverOpensAPathFromAClient(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.png")
	if err := os.WriteFile(secret, minimalPNG(), 0o600); err != nil {
		t.Fatal(err)
	}

	data, ok, err := screenshotPNG(map[string]interface{}{"screenshot_path": secret})
	if ok || err != nil || data != nil {
		t.Fatalf("a path must be ignored, got ok=%v err=%v len=%d", ok, err, len(data))
	}
}

func TestScreenshotPNGReadsOnlyTheBytesThatArrive(t *testing.T) {
	png := minimalPNG()
	status := map[string]interface{}{"screenshot_png_b64": base64.StdEncoding.EncodeToString(png)}

	data, ok, err := screenshotPNG(status)
	if err != nil || !ok {
		t.Fatalf("a valid PNG must be accepted: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(data, png) {
		t.Fatal("the decoded bytes must equal the bytes the client sent")
	}
}

func TestScreenshotPNGRefusesWhatIsNotAPNG(t *testing.T) {
	notPNG := base64.StdEncoding.EncodeToString([]byte("<html>not an image</html>"))
	if _, ok, err := screenshotPNG(map[string]interface{}{"screenshot_png_b64": notPNG}); ok || err == nil {
		t.Fatalf("non-PNG bytes must be refused, got ok=%v err=%v", ok, err)
	}
	if _, ok, err := screenshotPNG(map[string]interface{}{"screenshot_png_b64": "!!not base64!!"}); ok || err == nil {
		t.Fatalf("invalid base64 must be refused, got ok=%v err=%v", ok, err)
	}
}

func TestScreenshotPNGRefusesOversizeBytes(t *testing.T) {
	big := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, maxVisionImageBytes)...)
	encoded := base64.StdEncoding.EncodeToString(big)
	if _, ok, err := screenshotPNG(map[string]interface{}{"screenshot_png_b64": encoded}); ok || err == nil {
		t.Fatalf("a screenshot over the vision limit must be refused, got ok=%v err=%v", ok, err)
	}
}

func TestNoScreenshotIsNotAnError(t *testing.T) {
	if _, ok, err := screenshotPNG(map[string]interface{}{}); ok || err != nil {
		t.Fatalf("no screenshot is a normal state, got ok=%v err=%v", ok, err)
	}
}

// The vision request carries the image as base64 and never as a path.
func TestVisionRequestCarriesBytesNotAPath(t *testing.T) {
	received := make(chan map[string]interface{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		received <- body
		_, _ = w.Write([]byte(`{"summary":"ok"}`))
	}))
	defer server.Close()

	if _, err := summarizeWithVisionServer(server.URL, minimalPNG(), ""); err != nil {
		t.Fatalf("summarize failed: %v", err)
	}
	body := <-received
	if _, present := body["image_path"]; present {
		t.Fatal("the request must not name a path")
	}
	image, _ := body["image_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(image)
	if err != nil || !bytes.HasPrefix(decoded, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("image_base64 must decode to the PNG bytes: %v", err)
	}
}
