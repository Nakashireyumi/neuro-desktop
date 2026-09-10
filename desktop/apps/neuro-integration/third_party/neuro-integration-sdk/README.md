# Neuro Integration SDK for Go

A production-ready Go SDK for integrating games and applications with Neuro-sama.

## Features

- 🚀 Easy-to-use client for WebSocket communication
- 🎮 Clean action handler interface with validation and execution separation
- 🔄 Automatic action re-registration on reconnection
- 🎯 Action forcing with configurable priorities
- 🪟 Action windows for turn-based games
- 🔒 Thread-safe operations
- 📝 Comprehensive error handling
- 📚 Full example implementations

## Installation

```bash
go get github.com/cassitly/neuro-integration-sdk
```

### Dependencies

```bash
go get github.com/gorilla/websocket
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/cassitly/neuro-integration-sdk"
)

func main() {
    // Create client
    client, err := neuro.NewClient(neuro.ClientConfig{
        Game:         "My Game",
        WebsocketURL: "ws://localhost:8000",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Connect
    if err := client.Connect(); err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Send context
    client.SendContext("Game started!", false)

    // Register actions (see examples below)
    // ...
}
```

## Creating Actions

Actions implement the `ActionHandler` interface, which separates validation from execution:

### Simple Action (No Parameters)

```go
type GreetAction struct{}

func (a *GreetAction) GetName() string {
    return "greet"
}

func (a *GreetAction) GetDescription() string {
    return "Say hello to the player"
}

func (a *GreetAction) GetSchema() *neuro.ActionSchema {
    return nil // No parameters
}

func (a *GreetAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
    return nil, neuro.NewSuccessResult("Greeting!")
}

func (a *GreetAction) Execute(state interface{}) {
    log.Println("Hello!")
}

// Register it
client.RegisterAction(&GreetAction{})
```

### Action With Parameters

```go
type GiveItemAction struct{}

func (a *GiveItemAction) GetName() string {
    return "give_item"
}

func (a *GiveItemAction) GetDescription() string {
    return "Give an item to the player"
}

func (a *GiveItemAction) GetSchema() *neuro.ActionSchema {
    return neuro.WrapSchema(map[string]interface{}{
        "item": map[string]interface{}{
            "type":        "string",
            "description": "The item to give",
            "enum":        []string{"sword", "shield", "potion"},
        },
        "quantity": map[string]interface{}{
            "type":        "integer",
            "description": "How many to give",
            "default":     1,
            "minimum":     1,
            "maximum":     99,
        },
    }, []string{"item"}) // "item" is required
}

func (a *GiveItemAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
    var params struct {
        Item     string `json:"item"`
        Quantity int    `json:"quantity"`
    }

    if err := neuro.ParseActionData(data, &params); err != nil {
        return nil, neuro.NewFailureResult("Invalid parameters")
    }

    // Validate item
    validItems := map[string]bool{"sword": true, "shield": true, "potion": true}
    if !validItems[params.Item] {
        return nil, neuro.NewFailureResult("Invalid item")
    }

    return params, neuro.NewSuccessResult("Giving item")
}

func (a *GiveItemAction) Execute(state interface{}) {
    params := state.(struct {
        Item     string `json:"item"`
        Quantity int    `json:"quantity"`
    })
    
    log.Printf("Giving %d x %s\n", params.Quantity, params.Item)
}
```

## Action Windows (Turn-Based Games)

Action windows are perfect for turn-based games where you want to temporarily register and force specific actions:

```go
// Create a window
window := client.NewActionWindow()

// Add actions
window.AddAction(&PlayAction{game: game})
window.AddAction(&PassAction{game: game})

// Configure forcing
window.SetForce(
    "It's your turn. Choose your move.",
    neuro.WithPriority(neuro.PriorityMedium),
    neuro.WithEphemeralContext(false),
)

// Register and force the actions
if err := window.Register(); err != nil {
    log.Fatal(err)
}

// Later, when turn is over
defer window.End()
```

## Action Forcing

Force Neuro to choose from specific actions:

```go
client.ForceActions(
    "Choose an action",
    []string{"attack", "defend", "heal"},
    neuro.WithPriority(neuro.PriorityHigh),
    neuro.WithState("Player HP: 50/100"),
    neuro.WithEphemeralContext(true),
)
```

### Force Options

- `WithPriority(priority)` - Set priority: `PriorityLow`, `PriorityMedium`, `PriorityHigh`, `PriorityCritical`
- `WithState(state)` - Add state information for context
- `WithEphemeralContext(bool)` - Mark context as temporary

## Context Messages

Send context to inform Neuro about game state:

```go
// Regular context (stored in memory)
client.SendContext("The player entered a dark forest.", false)

// Silent context (not shown to chat)
client.SendContext("Internal game state: level=5", true)
```

## Managing Actions

```go
// Register single action
client.RegisterAction(handler)

// Register multiple actions
client.RegisterActions([]neuro.ActionHandler{handler1, handler2})

// Unregister action
client.UnregisterAction("action_name")

// Unregister multiple
client.UnregisterActions([]string{"action1", "action2"})
```

## Error Handling

```go
// Listen for errors
go func() {
    for err := range client.Errors() {
        log.Printf("Error: %v", err)
    }
}()
```

## Complete Example

See `example/main.go` for a complete working example with:
- Simple actions
- Actions with parameters and validation
- Stateful actions (Tic Tac Toe game)
- Action windows
- Error handling

## API Reference

### Client Methods

- `NewClient(config ClientConfig) (*Client, error)` - Create a new client
- `Connect() error` - Establish WebSocket connection
- `Close() error` - Close connection
- `Startup() error` - Send startup message
- `SendContext(message string, silent bool) error` - Send context
- `SendShutdownReady() error` - Signal ready to shutdown
- `RegisterAction(handler ActionHandler) error` - Register single action
- `RegisterActions(handlers []ActionHandler) error` - Register multiple actions
- `UnregisterAction(name string) error` - Unregister single action
- `UnregisterActions(names []string) error` - Unregister multiple actions
- `ForceActions(query string, actionNames []string, opts ...ForceOption) error` - Force action selection
- `SendActionResult(id string, success bool, message string) error` - Send action result
- `NewActionWindow() *ActionWindow` - Create action window
- `Errors() <-chan error` - Get error channel

### ActionHandler Interface

```go
type ActionHandler interface {
    GetName() string
    GetDescription() string
    GetSchema() *ActionSchema
    Validate(data json.RawMessage) (state interface{}, result ExecutionResult)
    Execute(state interface{})
}
```

### Helper Functions

- `WrapSchema(properties map[string]interface{}, required []string) *ActionSchema` - Create schema
- `ParseActionData(data json.RawMessage, v interface{}) error` - Parse action data
- `NewSuccessResult(message string) ExecutionResult` - Create success result
- `NewFailureResult(message string) ExecutionResult` - Create failure result

## Environment Variables

- `NEURO_SDK_WS_URL` - WebSocket URL (default: `ws://localhost:8000`)

## Important: Race Conditions & Action Timing

⚠️ **Critical**: Neuro can execute actions at **any time** after registration, even before you send an action force. Your action handlers must always be ready to handle actions.

### Recommendations from Official API Guide:

1. **Always listen for actions** - Don't assume actions only arrive after forcing
2. **Unregister disposable actions before sending result** - For turn-based games, unregister actions like "play_card" immediately in the Validate method, before returning the result
3. **Be careful with non-disposable actions** - If forcing actions that can be used multiple times, handle the possibility that Neuro might use them immediately before or after the force

### Action Windows Handle This Automatically

Action windows are designed to handle these race conditions for you in turn-based games by:
- Registering actions only when needed
- Automatically unregistering when done
- Managing the force/response lifecycle

## JSON Schema Limitations

The Neuro API has **limited JSON schema support**. The following keywords are probably **NOT supported**:

`$anchor`, `$comment`, `$defs`, `$dynamicAnchor`, `$dynamicRef`, `$id`, `$ref`, `$schema`, `$vocabulary`, `additionalProperties`, `allOf`, `anyOf`, `contentEncoding`, `contentMediaType`, `contentSchema`, `dependentRequired`, `dependentSchemas`, `deprecated`, `description`, `else`, `if`, `maxProperties`, `minProperties`, `multipleOf`, `not`, `oneOf`, `patternProperties`, `readOnly`, `then`, `title`, `unevaluatedItems`, `unevaluatedProperties`, `writeOnly`

**Note**: `uniqueItems` support is unknown - perform your own validation if you need it.

### Supported Keywords

Stick to these basic keywords:
- `type`, `properties`, `required`
- `enum`, `minimum`, `maximum`
- `minLength`, `maxLength`
- `pattern` (basic regex)
- `items` (for arrays)
- `default`

## Best Practices

1. **Always validate parameters** - Data from Neuro may be malformed or not match your schema
2. **Return meaningful messages** in `ExecutionResult` to help debug issues
3. **Keep Execute fast** - offload heavy work to background goroutines if needed
4. **Use action windows** for temporary, turn-based actions
5. **Handle errors** by listening to the error channel
6. **Clean up resources** with `defer client.Close()` and `defer window.End()`
7. **Validate even with schemas** - The API cannot guarantee schema compliance

## Thread Safety

All client methods are thread-safe and can be called from multiple goroutines.

## License

MIT

## Contributing

Contributions welcome! Please open an issue or PR.

## Support

For issues and questions, please open a GitHub issue.
