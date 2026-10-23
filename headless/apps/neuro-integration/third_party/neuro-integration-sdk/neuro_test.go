package neuro

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type pipelineAction struct {
	name     string
	validate func(json.RawMessage) (interface{}, ExecutionResult)
	execute  func(interface{})
}

func (a *pipelineAction) GetName() string        { return a.name }
func (*pipelineAction) GetDescription() string   { return "test action" }
func (*pipelineAction) GetSchema() *ActionSchema { return nil }
func (a *pipelineAction) Validate(data json.RawMessage) (interface{}, ExecutionResult) {
	if a.validate != nil {
		return a.validate(data)
	}
	return nil, NewSuccessResult("accepted")
}
func (a *pipelineAction) Execute(state interface{}) {
	if a.execute != nil {
		a.execute(state)
	}
}

func pipelineClient(t *testing.T) (*Client, chan Message, func(Message)) {
	t.Helper()
	frames := make(chan Message, 128)
	peer := make(chan *websocket.Conn, 1)
	var writers sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		peer <- conn
		defer conn.Close()
		for {
			var message Message
			if err := conn.ReadJSON(&message); err != nil {
				return
			}
			frames <- message
		}
	}))
	client, err := NewClient(ClientConfig{Game: "test", WebsocketURL: "ws" + strings.TrimPrefix(server.URL, "http"), Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	conn := <-peer
	t.Cleanup(func() { client.Close(); conn.Close(); server.Close() })
	return client, frames, func(message Message) {
		writers.Lock()
		defer writers.Unlock()
		if err := conn.WriteJSON(message); err != nil {
			t.Fatal(err)
		}
	}
}
func sendAction(t *testing.T, send func(Message), id, name string) {
	t.Helper()
	data, err := json.Marshal(IncomingAction{ID: id, Name: name, Data: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	send(Message{Command: "action", Data: data})
}
func nextResult(t *testing.T, frames <-chan Message) map[string]interface{} {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame := <-frames:
			if frame.Command != "action/result" {
				continue
			}
			var data map[string]interface{}
			if err := json.Unmarshal(frame.Data, &data); err != nil {
				t.Fatal(err)
			}
			return data
		case <-timer.C:
			t.Fatal("action acknowledgement did not arrive")
			return nil
		}
	}
}
func awaitSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal(message)
	}
}

func TestPipelineAcknowledgesAheadOfSerialExecution(t *testing.T) {
	client, frames, send := pipelineClient(t)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	action := &pipelineAction{name: "work", execute: func(interface{}) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		} else {
			close(finished)
		}
	}}
	if err := client.RegisterAction(action); err != nil {
		t.Fatal(err)
	}
	sendAction(t, send, "one", "work")
	if result := nextResult(t, frames); result["id"] != "one" || result["success"] != true {
		t.Fatalf("bad first ACK: %v", result)
	}
	awaitSignal(t, entered, "first execution did not start")
	sendAction(t, send, "two", "work")
	if result := nextResult(t, frames); result["id"] != "two" || result["success"] != true {
		t.Fatalf("slow execution blocked next ACK: %v", result)
	}
	if calls.Load() != 1 {
		t.Fatal("second execution bypassed the first")
	}
	close(release)
	awaitSignal(t, finished, "second execution did not follow the first")
}

func TestQueueCapacityControlResponsivenessAndClose(t *testing.T) {
	client, frames, send := pipelineClient(t)
	entered, release, control := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls, validations atomic.Int32
	action := &pipelineAction{name: "work", validate: func(json.RawMessage) (interface{}, ExecutionResult) {
		validations.Add(1)
		return nil, NewSuccessResult("accepted")
	}, execute: func(interface{}) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}}
	if err := client.RegisterAction(action); err != nil {
		t.Fatal(err)
	}
	client.OnCommand("shutdown/immediate", func(json.RawMessage) { close(control) })
	for i := 0; i < actionQueueCapacity; i++ {
		sendAction(t, send, "queued", "work")
		if result := nextResult(t, frames); result["success"] != true {
			t.Fatalf("queue rejected available slot %d: %v", i, result)
		}
	}
	awaitSignal(t, entered, "execution never started")
	sendAction(t, send, "overflow", "work")
	if result := nextResult(t, frames); result["id"] != "overflow" || result["success"] != false {
		t.Fatalf("full queue did not reject overflow: %v", result)
	}
	if validations.Load() != actionQueueCapacity {
		t.Fatal("rejected action performed hidden validation work")
	}
	send(Message{Command: "shutdown/immediate"})
	awaitSignal(t, control, "raw shutdown waited behind action execution")
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitSignal(t, client.workersDone, "workers did not drain on close")
	if calls.Load() != 1 {
		t.Fatalf("close executed queued work: %d", calls.Load())
	}
	if len(client.workSlots) != 0 {
		t.Fatalf("close leaked %d work slots", len(client.workSlots))
	}
}

func TestAcknowledgementWriteFailurePreventsExecution(t *testing.T) {
	client, err := NewClient(ClientConfig{Game: "test", WebsocketURL: "ws://127.0.0.1:1", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	validated := make(chan struct{})
	client.actions["work"] = &pipelineAction{name: "work", validate: func(json.RawMessage) (interface{}, ExecutionResult) {
		close(validated)
		return nil, NewSuccessResult("accepted")
	}, execute: func(interface{}) { calls.Add(1) }}
	client.startActionWorkers()
	if !client.admit(incomingWork{action: &IncomingAction{ID: "no-connection", Name: "work"}}) {
		t.Fatal("admission failed")
	}
	awaitSignal(t, validated, "validation did not finish")
	select {
	case <-client.Errors():
	case <-time.After(3 * time.Second):
		t.Fatal("failed ACK was not reported")
	}
	client.Close()
	awaitSignal(t, client.workersDone, "workers did not finish")
	if calls.Load() != 0 {
		t.Fatal("action executed even though its acknowledgement failed")
	}
}

func TestControlQueueIsBounded(t *testing.T) {
	client, err := NewClient(ClientConfig{Game: "test", WebsocketURL: "ws://127.0.0.1:1", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	// Without starting workers, admission deterministically fills the fixed queue.
	for i := 0; i < cap(client.controls); i++ {
		client.admitControl(func() { t.Error("closed control command executed") })
	}
	client.admitControl(func() { t.Error("overflow control executed") })
	if !client.isClosed() {
		t.Fatal("control overflow did not close the connection")
	}
}
