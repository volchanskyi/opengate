package authority

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// serviceAccountDir is where Kubernetes mounts a pod's own credentials.
const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// requestBudget bounds one call to the Kubernetes API.
const requestBudget = 30 * time.Second

// Secrets reads and replaces Kubernetes secrets as the pod's own service account, which is the
// only path the master key takes between the cluster's store and this tool.
type Secrets struct {
	base      string
	namespace string
	token     string
	client    *http.Client
}

// InCluster connects with the pod's mounted service-account credentials.
func InCluster() (*Secrets, error) {
	return inCluster(serviceAccountDir)
}

// inCluster connects with the service-account credentials mounted at dir.
func inCluster(dir string) (*Secrets, error) {
	mount, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open service-account mount: %w", err)
	}
	defer mount.Close()
	read := mount.ReadFile
	token, err := read("token")
	if err != nil {
		return nil, fmt.Errorf("read service-account token: %w", err)
	}
	namespace, err := read("namespace")
	if err != nil {
		return nil, fmt.Errorf("read service-account namespace: %w", err)
	}
	caPEM, err := read("ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read cluster authority: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("the cluster authority does not parse")
	}
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not inside a cluster: KUBERNETES_SERVICE_HOST is unset")
	}
	client := &http.Client{
		Timeout:   requestBudget,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}
	return NewSecrets("https://"+host+":"+port, strings.TrimSpace(string(namespace)),
		strings.TrimSpace(string(token)), client), nil
}

// NewSecrets talks to the API server at base about secrets in namespace, presenting token.
func NewSecrets(base, namespace, token string, client *http.Client) *Secrets {
	return &Secrets{base: strings.TrimRight(base, "/"), namespace: namespace, token: token, client: client}
}

// secretObject is the part of a Kubernetes Secret this tool reads and writes.
type secretObject struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   secretMetadata    `json:"metadata"`
	Type       string            `json:"type,omitempty"`
	Data       map[string][]byte `json:"data"`
}

type secretMetadata struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// Read returns the secret's files, and false when it does not exist.
func (s *Secrets) Read(ctx context.Context, name string) (Files, bool, error) {
	obj, found, err := s.get(ctx, name)
	if err != nil || !found {
		return nil, found, err
	}
	return Files(obj.Data), true, nil
}

// Write replaces the secret's files, creating it when absent; a write that races another is
// refused by the server on the resource version it read.
func (s *Secrets) Write(ctx context.Context, name string, files Files) error {
	existing, found, err := s.get(ctx, name)
	if err != nil {
		return err
	}
	obj := secretObject{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata:   secretMetadata{Name: name, Namespace: s.namespace},
		Type:       "Opaque",
		Data:       map[string][]byte(files),
	}
	method, url := http.MethodPost, s.collection()
	if found {
		obj.Metadata.ResourceVersion = existing.Metadata.ResourceVersion
		method, url = http.MethodPut, s.collection()+"/"+name
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode secret: %w", err)
	}
	resp, err := s.do(ctx, method, url, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return apiError("write", name, resp)
	}
	return s.readBack(ctx, name, files)
}

// readBack refuses a write the store did not keep whole, naming the file and never its content.
func (s *Secrets) readBack(ctx context.Context, name string, want Files) error {
	got, found, err := s.Read(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("read back secret %s: it is absent after the write", name)
	}
	for _, file := range append(want.Names(), got.Names()...) {
		if !bytes.Equal(want[file], got[file]) {
			return fmt.Errorf("read back secret %s: %s differs from what was written", name, file)
		}
	}
	return nil
}

// RemoveKeys deletes the named keys from a secret and leaves its others; a key already gone is
// no error, so an interrupted move runs again cleanly.
func (s *Secrets) RemoveKeys(ctx context.Context, name string, keys ...string) error {
	data := map[string]any{}
	for _, key := range keys {
		data[key] = nil
	}
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return fmt.Errorf("encode patch: %w", err)
	}
	resp, err := s.doTyped(ctx, http.MethodPatch, s.collection()+"/"+name, body, "application/merge-patch+json")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return apiError("patch", name, resp)
	}
	return nil
}

func (s *Secrets) get(ctx context.Context, name string) (secretObject, bool, error) {
	resp, err := s.do(ctx, http.MethodGet, s.collection()+"/"+name, nil)
	if err != nil {
		return secretObject{}, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return secretObject{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return secretObject{}, false, apiError("read", name, resp)
	}
	var obj secretObject
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return secretObject{}, false, fmt.Errorf("decode secret %s: %w", name, err)
	}
	return obj, true, nil
}

func (s *Secrets) collection() string {
	return s.base + "/api/v1/namespaces/" + s.namespace + "/secrets"
}

func (s *Secrets) do(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	return s.doTyped(ctx, method, url, body, "application/json")
}

func (s *Secrets) doTyped(ctx context.Context, method, url string, body []byte, contentType string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", contentType)
	resp, err := s.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnClose ends a request's deadline once its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}

// apiError names a refused call by status, never by the body, which may echo the secret.
func apiError(verb, name string, resp *http.Response) error {
	return fmt.Errorf("%s secret %s: the API server answered %s", verb, name, resp.Status)
}
