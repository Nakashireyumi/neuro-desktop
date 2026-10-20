package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
)

// Catalog trust.
//
// The catalog index is the only list of things Neuro Desktop will install. An
// item is "verified" when its signature checks out against a key in
// publishers.json. The signature covers the item's JSON minus the signature
// field, with keys sorted and no insignificant whitespace. Changing a
// description, repository, or commit after signing therefore breaks it.
//
// States:
//
//	verified   signed, and the signature matches a listed publisher key
//	unsigned   no signature field
//	untrusted  signed by a publisher that publishers.json does not list
//	invalid    the signature is malformed or does not match the item
//
// Operator commands (run from the repository root):
//
//	neuro-integration catalog keygen --publisher ID --out key.json
//	neuro-integration catalog sign   --key key.json [--index catalog/index.json]
//	neuro-integration catalog verify [--index ...] [--publishers ...] [--allow-unsigned]
//
// The private key file must never be committed. Only the public key goes into
// publishers.json.

type publisherKey struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	PublicKey string `json:"public_key"`
}

type publisherFile struct {
	Note       string         `json:"note,omitempty"`
	Publishers []publisherKey `json:"publishers"`
}

type signingKeyFile struct {
	Publisher  string `json:"publisher"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// signatureStatus is the trust state of one catalog item.
type signatureStatus struct {
	State     string `json:"state"`
	Publisher string `json:"publisher,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

var publisherIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

const catalogUsage = `usage:
  neuro-integration catalog keygen --publisher ID --out key.json
  neuro-integration catalog sign   --key key.json [--index catalog/index.json]
  neuro-integration catalog verify [--index catalog/index.json] [--publishers catalog/publishers.json] [--allow-unsigned]`

func publishersFilePath() string {
	if path := strings.TrimSpace(os.Getenv("NEURO_PUBLISHERS_FILE")); path != "" {
		return path
	}
	return "./catalog/publishers.json"
}

func loadPublishers(path string) (publisherFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return publisherFile{}, nil
		}
		return publisherFile{}, fmt.Errorf("could not read publishers file %s: %w", path, err)
	}
	var file publisherFile
	if err := json.Unmarshal(data, &file); err != nil {
		return publisherFile{}, fmt.Errorf("publishers file %s is not valid JSON: %w", path, err)
	}
	return file, nil
}

// loadPublishersOrEmpty is what the bridge uses at install time. A broken file
// trusts nobody, which is the safe failure, and it says so in the log.
func loadPublishersOrEmpty() publisherFile {
	file, err := loadPublishers(publishersFilePath())
	if err != nil {
		log.Printf("WARNING: %v; no publisher is trusted until it is fixed", err)
		return publisherFile{}
	}
	return file
}

// canonicalItemBytes is the byte string that is signed and verified.
func canonicalItemBytes(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("the item has no JSON to verify")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var fields map[string]interface{}
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("the item is not a JSON object: %w", err)
	}
	delete(fields, "signature")
	return json.Marshal(fields)
}

// verifyCatalogItem returns the trust state of one item. It never fetches
// anything and never trusts the item's own claims about who signed it.
func verifyCatalogItem(item CatalogItem, publishers publisherFile) signatureStatus {
	status := signatureStatus{Publisher: item.Publisher}

	if strings.TrimSpace(item.Signature) == "" {
		status.State = "unsigned"
		status.Detail = "no signature"
		return status
	}
	if strings.TrimSpace(item.Publisher) == "" {
		status.State = "untrusted"
		status.Detail = "signed, but names no publisher"
		return status
	}

	var key *publisherKey
	for i := range publishers.Publishers {
		if publishers.Publishers[i].ID == item.Publisher {
			key = &publishers.Publishers[i]
			break
		}
	}
	if key == nil {
		status.State = "untrusted"
		status.Detail = fmt.Sprintf("publisher %q is not listed in publishers.json", item.Publisher)
		return status
	}

	publicKey, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		status.State = "invalid"
		status.Detail = fmt.Sprintf("publisher %q has a malformed public key", item.Publisher)
		return status
	}
	signature, err := base64.StdEncoding.DecodeString(item.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		status.State = "invalid"
		status.Detail = "the signature is malformed"
		return status
	}
	message, err := canonicalItemBytes(item.Raw)
	if err != nil {
		status.State = "invalid"
		status.Detail = err.Error()
		return status
	}
	if !ed25519.Verify(publicKey, message, signature) {
		status.State = "invalid"
		status.Detail = "the signature does not match this item"
		return status
	}

	status.State = "verified"
	return status
}

// runCatalogCommand implements `neuro-integration catalog ...`. It returns the
// process exit code.
func runCatalogCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, catalogUsage)
		return 2
	}
	switch args[0] {
	case "keygen":
		return catalogKeygen(args[1:], stdout, stderr)
	case "sign":
		return catalogSign(args[1:], stdout, stderr)
	case "verify":
		return catalogVerify(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown catalog command %q\n%s\n", args[0], catalogUsage)
	return 2
}

func catalogKeygen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog keygen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	publisher := fs.String("publisher", "", "publisher id, for example my-studio")
	out := fs.String("out", "", "file to write the private key to (never commit it)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	id := strings.TrimSpace(*publisher)
	if !publisherIDPattern.MatchString(id) || *out == "" {
		fmt.Fprintln(stderr, "catalog keygen needs --publisher (lowercase id) and --out")
		return 2
	}
	if _, err := os.Stat(*out); err == nil {
		fmt.Fprintf(stderr, "refusing to overwrite %s\n", *out)
		return 1
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(stderr, "could not generate a key: %v\n", err)
		return 1
	}
	file := signingKeyFile{
		Publisher:  id,
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "could not encode the key: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0600); err != nil {
		fmt.Fprintf(stderr, "could not write %s: %v\n", *out, err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote the private key to %s (keep it secret; do not commit it)\n", *out)
	fmt.Fprintf(stdout, "add this entry to catalog/publishers.json:\n  {\"id\": %q, \"name\": %q, \"public_key\": %q}\n", id, id, file.PublicKey)
	return 0
}

// loadSigningKey reads a key file and checks that the public and private halves
// belong together before anything is signed with it.
func loadSigningKey(path string) (signingKeyFile, ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return signingKeyFile{}, nil, fmt.Errorf("could not read key file %s: %w", path, err)
	}
	var key signingKeyFile
	if err := json.Unmarshal(data, &key); err != nil {
		return signingKeyFile{}, nil, fmt.Errorf("key file %s is not valid JSON: %w", path, err)
	}
	if !publisherIDPattern.MatchString(key.Publisher) {
		return signingKeyFile{}, nil, fmt.Errorf("key file %s has an invalid publisher id", path)
	}
	private, err := base64.StdEncoding.DecodeString(key.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return signingKeyFile{}, nil, fmt.Errorf("key file %s has a malformed private key", path)
	}
	derived := ed25519.PrivateKey(private).Public().(ed25519.PublicKey)
	if base64.StdEncoding.EncodeToString(derived) != key.PublicKey {
		return signingKeyFile{}, nil, fmt.Errorf("key file %s: the public key does not match the private key", path)
	}
	return key, ed25519.PrivateKey(private), nil
}

// catalogSign signs every item that names the key's publisher, and rewrites the
// index in place. Items from other publishers are left alone.
func catalogSign(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog sign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	keyPath := fs.String("key", "", "signing key written by `catalog keygen`")
	indexPath := fs.String("index", "catalog/index.json", "catalog index to sign (rewritten in place)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *keyPath == "" {
		fmt.Fprintln(stderr, "catalog sign needs --key")
		return 2
	}

	key, private, err := loadSigningKey(*keyPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	data, err := os.ReadFile(*indexPath)
	if err != nil {
		fmt.Fprintf(stderr, "could not read %s: %v\n", *indexPath, err)
		return 1
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var doc map[string]interface{}
	if err := decoder.Decode(&doc); err != nil {
		fmt.Fprintf(stderr, "%s is not valid JSON: %v\n", *indexPath, err)
		return 1
	}
	items, ok := doc["items"].([]interface{})
	if !ok {
		fmt.Fprintf(stderr, "%s has no items array\n", *indexPath)
		return 1
	}

	signed := 0
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if publisher, _ := item["publisher"].(string); publisher != key.Publisher {
			continue
		}
		delete(item, "signature")
		body, err := json.Marshal(item)
		if err != nil {
			fmt.Fprintf(stderr, "could not encode an item: %v\n", err)
			return 1
		}
		item["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))
		signed++
	}
	if signed == 0 {
		fmt.Fprintf(stderr, "no item in %s names publisher %q; nothing signed\n", *indexPath, key.Publisher)
		return 1
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "could not encode the index: %v\n", err)
		return 1
	}
	if err := atomicWriteFile(*indexPath, append(out, '\n')); err != nil {
		fmt.Fprintf(stderr, "could not write %s: %v\n", *indexPath, err)
		return 1
	}

	fmt.Fprintf(stdout, "signed %d item(s) as %s in %s\n", signed, key.Publisher, *indexPath)
	return 0
}

// catalogVerify checks every item. By default it fails on anything that is not
// verified, which is what a release gate needs. --allow-unsigned lets unsigned
// (but not invalid or untrusted) items pass, for metadata-only catalogs.
func catalogVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	indexPath := fs.String("index", "catalog/index.json", "catalog index to verify")
	publishersPath := fs.String("publishers", publishersFilePath(), "publisher keys file")
	allowUnsigned := fs.Bool("allow-unsigned", false, "accept items that have no signature")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	index, err := loadCatalogIndex(*indexPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	publishers, err := loadPublishers(*publishersPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	failed := 0
	for _, item := range index.Items {
		status := verifyCatalogItem(item, publishers)
		line := fmt.Sprintf("%-22s %-9s", item.ID, status.State)
		if status.Detail != "" {
			line += " " + status.Detail
		}
		fmt.Fprintln(stdout, line)

		switch status.State {
		case "verified":
		case "unsigned":
			if !*allowUnsigned {
				failed++
			}
		default:
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(stderr, "%d item(s) are not acceptable\n", failed)
		return 1
	}
	fmt.Fprintf(stdout, "%d item(s) checked\n", len(index.Items))
	return 0
}
