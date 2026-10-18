package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stalledAdapterBody struct {
	entered, release chan struct{}
	once             sync.Once
	reader           io.Reader
}

func (b *stalledAdapterBody) Read(data []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	if b.reader != nil {
		return b.reader.Read(data)
	}
	return 0, io.EOF
}

type stalledAdapterResponse struct {
	entered, release chan struct{}
	header           http.Header
	once             sync.Once
}

func TestAdapterRechecksPauseAfterReceivingSlowBody(t *testing.T) {
	n, a, m := adapterFixture(t)
	token := adapterCredential(t, a, m.ID, "integration")
	body := &stalledAdapterBody{entered: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader(`{"state":"ready"}`)}
	r := httptest.NewRequest("POST", "/api/integrations/"+m.ID+"/heartbeat", nil)
	r.Body = io.NopCloser(body)
	r.Header.Set("X-ND-Integration-Token", token)
	w := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() { defer close(finished); a.routes().ServeHTTP(w, r) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		close(body.release)
		t.Fatal("request did not start reading its body")
	}
	n.stop.setPaused(true)
	close(body.release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("request received before pause executed after pause: %d %s", w.Code, w.Body.String())
	}
}

func (w *stalledAdapterResponse) Header() http.Header { return w.header }
func (w *stalledAdapterResponse) WriteHeader(int)     {}
func (w *stalledAdapterResponse) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func TestSlowAdapterIOCannotBlockOtherRuntimeOperations(t *testing.T) {
	for _, phase := range []string{"request body", "response write"} {
		t.Run(phase, func(t *testing.T) {
			n, a, m := adapterFixture(t)
			token := adapterCredential(t, a, m.ID, "integration")
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			r := httptest.NewRequest("POST", "/api/integrations/"+m.ID+"/heartbeat", strings.NewReader(`{"state":"ready"}`))
			r.Header.Set("X-ND-Integration-Token", token)
			var response http.ResponseWriter = httptest.NewRecorder()
			if phase == "request body" {
				r.Body = io.NopCloser(&stalledAdapterBody{entered: entered, release: release})
			} else {
				response = &stalledAdapterResponse{entered: entered, release: release, header: http.Header{}}
			}
			go func() { defer close(finished); a.routes().ServeHTTP(response, r) }()
			defer func() {
				close(release)
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("stalled request did not finish after release")
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach the controlled I/O stall")
			}
			hidden := make(chan struct{})
			go func() { n.hideAdapter(m.ID); close(hidden) }()
			select {
			case <-hidden:
			case <-time.After(time.Second):
				t.Fatal("one stalled HTTP stream held the shared runtime mutex")
			}
		})
	}
}

func TestReapprovingViewerPermissionCannotRestoreEarlierSharedHistory(t *testing.T) {
	n, a, m := adapterFixture(t)
	token := adapterCredential(t, a, m.ID, "integration")
	viewer := adapterCredential(t, a, m.ID, "viewer")
	headers := map[string]string{"X-ND-Integration-Token": token}
	adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, headers)
	if err := n.setViewerVisibility(m.ID, "neuro", true, 15); err != nil {
		t.Fatal(err)
	}
	code, _ := adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/activity", map[string]string{"actor": "neuro", "kind": "status", "summary": "Previously shared event"}, headers)
	if code != 200 {
		t.Fatal("fixture event failed")
	}
	for _, granted := range []bool{false, true} {
		code, body := adapterRequest(t, a, "PUT", "/api/extensions/"+m.ID+"/permissions", ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{"discord.viewer": granted}, Settings: map[string]interface{}{}}, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
		if code != 200 {
			t.Fatalf("permission update: %d %v", code, body)
		}
	}
	// No viewer request happens during revocation. Restoring permission alone
	// must not restore disclosure consent or any earlier content.
	_, feed := adapterRequest(t, a, "GET", "/api/viewer/"+m.ID, nil, map[string]string{"X-ND-Viewer-Token": viewer})
	if feed["visible"] != false || len(feed["items"].([]interface{})) != 0 {
		t.Fatalf("reapproval restored stale history: %v", feed)
	}
}
