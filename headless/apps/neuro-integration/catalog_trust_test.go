package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signedCatalogFixture writes a key, a publishers file, and an index with one
// item from "acme", returning the paths.
func signedCatalogFixture(t *testing.T, dir string) (keyPath, indexPath, publishersPath string) {
	t.Helper()
	keyPath = filepath.Join(dir, "acme-key.json")
	indexPath = filepath.Join(dir, "index.json")
	publishersPath = filepath.Join(dir, "publishers.json")

	var out, errOut bytes.Buffer
	if code := catalogKeygen([]string{"--publisher", "acme", "--out", keyPath}, &out, &errOut); code != 0 {
		t.Fatalf("keygen failed (%d): %s", code, errOut.String())
	}

	key, _, err := loadSigningKey(keyPath)
	if err != nil {
		t.Fatalf("loadSigningKey: %v", err)
	}
	publishers := publisherFile{Publishers: []publisherKey{{ID: "acme", Name: "Acme", PublicKey: key.PublicKey}}}
	writeJSONFile(t, publishersPath, publishers)

	index := `{
  "items": [
    {
      "id": "acme-tools",
      "name": "Acme Tools",
      "description": "Signed test item",
      "repository": "https://example.com/acme/tools",
      "publisher": "acme",
      "commit": "0123456789abcdef0123456789abcdef01234567"
    }
  ]
}
`
	if err := os.WriteFile(indexPath, []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	return keyPath, indexPath, publishersPath
}

func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyPath, indexPath, publishersPath := signedCatalogFixture(t, dir)

	var out, errOut bytes.Buffer
	if code := catalogSign([]string{"--key", keyPath, "--index", indexPath}, &out, &errOut); code != 0 {
		t.Fatalf("sign failed (%d): %s", code, errOut.String())
	}

	out.Reset()
	if code := catalogVerify([]string{"--index", indexPath, "--publishers", publishersPath}, &out, &errOut); code != 0 {
		t.Fatalf("verify failed (%d): %s / %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "verified") {
		t.Fatalf("expected the item to verify, got: %s", out.String())
	}

	index, err := loadCatalogIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	status := verifyCatalogItem(index.Items[0], mustLoadPublishers(t, publishersPath))
	if status.State != "verified" {
		t.Fatalf("expected verified, got %+v", status)
	}
}

func TestTamperedItemIsInvalid(t *testing.T) {
	dir := t.TempDir()
	keyPath, indexPath, publishersPath := signedCatalogFixture(t, dir)

	var out, errOut bytes.Buffer
	if code := catalogSign([]string{"--key", keyPath, "--index", indexPath}, &out, &errOut); code != 0 {
		t.Fatalf("sign failed: %s", errOut.String())
	}

	// Someone edits the signed item after the fact: a new repository.
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), "https://example.com/acme/tools", "https://evil.example/tools", 1)
	if tampered == string(data) {
		t.Fatalf("test precondition: repository must appear in the index")
	}
	if err := os.WriteFile(indexPath, []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := catalogVerify([]string{"--index", indexPath, "--publishers", publishersPath}, &out, &errOut); code == 0 {
		t.Fatalf("verify must fail on a tampered item")
	}
	if !strings.Contains(out.String(), "invalid") {
		t.Fatalf("expected an invalid state in the report, got: %s", out.String())
	}

	index, _ := loadCatalogIndex(indexPath)
	status := verifyCatalogItem(index.Items[0], mustLoadPublishers(t, publishersPath))
	if status.State != "invalid" {
		t.Fatalf("expected invalid, got %+v", status)
	}
}

func TestUnknownPublisherIsUntrusted(t *testing.T) {
	dir := t.TempDir()
	keyPath, indexPath, _ := signedCatalogFixture(t, dir)

	var out, errOut bytes.Buffer
	if code := catalogSign([]string{"--key", keyPath, "--index", indexPath}, &out, &errOut); code != 0 {
		t.Fatalf("sign failed: %s", errOut.String())
	}

	emptyPublishers := filepath.Join(dir, "none.json")
	writeJSONFile(t, emptyPublishers, publisherFile{})

	index, _ := loadCatalogIndex(indexPath)
	status := verifyCatalogItem(index.Items[0], mustLoadPublishers(t, emptyPublishers))
	if status.State != "untrusted" {
		t.Fatalf("a publisher missing from publishers.json must be untrusted, got %+v", status)
	}
}

func TestUnsignedItemsNeedTheAllowFlag(t *testing.T) {
	dir := t.TempDir()
	_, indexPath, publishersPath := signedCatalogFixture(t, dir)

	var out, errOut bytes.Buffer
	if code := catalogVerify([]string{"--index", indexPath, "--publishers", publishersPath}, &out, &errOut); code == 0 {
		t.Fatalf("an unsigned item must fail the default (strict) verify")
	}
	if code := catalogVerify([]string{"--index", indexPath, "--publishers", publishersPath, "--allow-unsigned"}, &out, &errOut); code != 0 {
		t.Fatalf("--allow-unsigned should accept unsigned items, got %d: %s", code, errOut.String())
	}
}

func TestSignOnlyTouchesItsOwnPublisher(t *testing.T) {
	dir := t.TempDir()
	keyPath, indexPath, _ := signedCatalogFixture(t, dir)

	index := `{"items": [
  {"id": "acme-tools", "name": "A", "description": "a", "publisher": "acme", "commit": "0123456789abcdef0123456789abcdef01234567", "repository": "https://example.com/a"},
  {"id": "other-tools", "name": "O", "description": "o", "publisher": "other", "commit": "0123456789abcdef0123456789abcdef01234567", "repository": "https://example.com/o"}
]}`
	if err := os.WriteFile(indexPath, []byte(index), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := catalogSign([]string{"--key", keyPath, "--index", indexPath}, &out, &errOut); code != 0 {
		t.Fatalf("sign failed: %s", errOut.String())
	}
	loaded, _ := loadCatalogIndex(indexPath)
	for _, item := range loaded.Items {
		if item.ID == "other-tools" && item.Signature != "" {
			t.Fatalf("signing as acme must not sign another publisher's item")
		}
		if item.ID == "acme-tools" && item.Signature == "" {
			t.Fatalf("the acme item should have been signed")
		}
	}
}

func TestSignRefusesWhenNothingMatches(t *testing.T) {
	dir := t.TempDir()
	keyPath, indexPath, _ := signedCatalogFixture(t, dir)
	index := `{"items": [{"id": "x", "name": "x", "description": "x", "publisher": "someone-else"}]}`
	if err := os.WriteFile(indexPath, []byte(index), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := catalogSign([]string{"--key", keyPath, "--index", indexPath}, &out, &errOut); code == 0 {
		t.Fatalf("sign with no matching item must fail rather than report success")
	}
}

func TestKeygenRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	var out, errOut bytes.Buffer
	if code := catalogKeygen([]string{"--publisher", "acme", "--out", keyPath}, &out, &errOut); code != 0 {
		t.Fatalf("first keygen failed: %s", errOut.String())
	}
	if code := catalogKeygen([]string{"--publisher", "acme", "--out", keyPath}, &out, &errOut); code == 0 {
		t.Fatalf("keygen must not overwrite an existing private key")
	}
}

func TestLoadSigningKeyRejectsMismatchedPair(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	var out, errOut bytes.Buffer
	if code := catalogKeygen([]string{"--publisher", "acme", "--out", keyPath}, &out, &errOut); code != 0 {
		t.Fatalf("keygen failed: %s", errOut.String())
	}
	key, _, err := loadSigningKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(dir, "other.json")
	if code := catalogKeygen([]string{"--publisher", "acme", "--out", other}, &out, &errOut); code != 0 {
		t.Fatalf("second keygen failed: %s", errOut.String())
	}
	otherKey, _, _ := loadSigningKey(other)

	key.PublicKey = otherKey.PublicKey
	writeJSONFile(t, keyPath, key)
	if _, _, err := loadSigningKey(keyPath); err == nil {
		t.Fatalf("a public key that does not match the private key must be rejected")
	}
}

func TestCanonicalFormIgnoresOrderAndSignature(t *testing.T) {
	a := []byte(`{"id":"x","name":"X","signature":"AAAA","tags":["a","b"],"nested":{"b":1,"a":2}}`)
	b := []byte("{\n  \"nested\": {\"a\": 2, \"b\": 1},\n  \"tags\": [\"a\", \"b\"],\n  \"id\": \"x\",\n  \"name\": \"X\"\n}")

	ca, err := canonicalItemBytes(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := canonicalItemBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("canonical form differs:\n%s\n%s", ca, cb)
	}
	if strings.Contains(string(ca), "signature") {
		t.Fatalf("the signature must not be part of what gets signed: %s", ca)
	}
}

func TestItemWithoutRawJSONIsInvalidNotVerified(t *testing.T) {
	publishers := publisherFile{Publishers: []publisherKey{{ID: "acme", PublicKey: strings.Repeat("A", 43) + "="}}}
	item := CatalogItem{ID: "x", Publisher: "acme", Signature: strings.Repeat("A", 86) + "=="}
	status := verifyCatalogItem(item, publishers)
	if status.State == "verified" {
		t.Fatalf("an item with no raw bytes must never verify")
	}
}

func mustLoadPublishers(t *testing.T, path string) publisherFile {
	t.Helper()
	file, err := loadPublishers(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
