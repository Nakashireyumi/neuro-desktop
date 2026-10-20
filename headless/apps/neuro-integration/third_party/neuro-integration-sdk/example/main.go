package example

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cassitly/neuro-integration-sdk"
)

// Example 1: Simple action with no parameters
type GreetAction struct{}

func (a *GreetAction) GetName() string {
	return "greet"
}

func (a *GreetAction) GetDescription() string {
	return "Say hello to the player"
}

func (a *GreetAction) GetSchema() *neuro.ActionSchema {
	return nil // No parameters needed
}

func (a *GreetAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
	return nil, neuro.NewSuccessResult("Greeting the player!")
}

func (a *GreetAction) Execute(state interface{}) {
	log.Println("👋 Hello from Neuro!")
}

// Example 2: Action with parameters and validation
type GiveItemAction struct {
	validItems map[string]bool
}

func NewGiveItemAction() *GiveItemAction {
	return &GiveItemAction{
		validItems: map[string]bool{
			"sword":   true,
			"shield":  true,
			"potion":  true,
			"cookies": true,
		},
	}
}

func (a *GiveItemAction) GetName() string {
	return "give_item"
}

func (a *GiveItemAction) GetDescription() string {
	return "Give an item to the player"
}

func (a *GiveItemAction) GetSchema() *neuro.ActionSchema {
	items := make([]string, 0, len(a.validItems))
	for item := range a.validItems {
		items = append(items, item)
	}

	return neuro.WrapSchema(map[string]interface{}{
		"item": map[string]interface{}{
			"type":        "string",
			"description": "The item to give",
			"enum":        items,
		},
		"quantity": map[string]interface{}{
			"type":        "integer",
			"description": "How many to give",
			"default":     1,
			"minimum":     1,
			"maximum":     99,
		},
	}, []string{"item"})
}

func (a *GiveItemAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
	var params struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}

	if err := neuro.ParseActionData(data, &params); err != nil {
		return nil, neuro.NewFailureResult("Invalid parameters")
	}

	if !a.validItems[params.Item] {
		return nil, neuro.NewFailureResult("Invalid item: " + params.Item)
	}

	if params.Quantity <= 0 {
		params.Quantity = 1
	}

	return params, neuro.NewSuccessResult("Giving item to player")
}

func (a *GiveItemAction) Execute(state interface{}) {
	params := state.(struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	})

	log.Printf("🎁 Giving %d x %s to the player\n", params.Quantity, params.Item)
}

// Example 3: Stateful action (Tic Tac Toe)
type TicTacToeGame struct {
	board  [9]string
	client *neuro.Client
}

func NewTicTacToeGame(client *neuro.Client) *TicTacToeGame {
	return &TicTacToeGame{
		board:  [9]string{},
		client: client,
	}
}

func (g *TicTacToeGame) GetAvailableCells() []string {
	available := make([]string, 0)
	for i, cell := range g.board {
		if cell == "" {
			available = append(available, string(rune('1'+i)))
		}
	}
	return available
}

func (g *TicTacToeGame) IsCellAvailable(cell string) bool {
	if len(cell) != 1 {
		return false
	}
	idx := int(cell[0] - '1')
	if idx < 0 || idx > 8 {
		return false
	}
	return g.board[idx] == ""
}

func (g *TicTacToeGame) PlayInCell(cell string) {
	idx := int(cell[0] - '1')
	g.board[idx] = "O"
	log.Printf("Neuro played O in cell %s", cell)
	g.printBoard()
}

func (g *TicTacToeGame) printBoard() {
	log.Println("Current board:")
	for i := 0; i < 9; i += 3 {
		row := ""
		for j := 0; j < 3; j++ {
			if g.board[i+j] == "" {
				row += string(rune('1' + i + j))
			} else {
				row += g.board[i+j]
			}
			if j < 2 {
				row += " | "
			}
		}
		log.Println(row)
		if i < 6 {
			log.Println("---------")
		}
	}
}

type PlayAction struct {
	game *TicTacToeGame
}

func (a *PlayAction) GetName() string {
	return "play"
}

func (a *PlayAction) GetDescription() string {
	return "Place an O in the specified cell"
}

func (a *PlayAction) GetSchema() *neuro.ActionSchema {
	return neuro.WrapSchema(map[string]interface{}{
		"cell": map[string]interface{}{
			"type":        "string",
			"description": "Cell number (1-9)",
			"enum":        a.game.GetAvailableCells(),
		},
	}, []string{"cell"})
}

func (a *PlayAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
	var params struct {
		Cell string `json:"cell"`
	}

	if err := neuro.ParseActionData(data, &params); err != nil {
		return nil, neuro.NewFailureResult("Invalid action data")
	}

	if params.Cell == "" {
		return nil, neuro.NewFailureResult("Missing required parameter 'cell'")
	}

	if !a.game.IsCellAvailable(params.Cell) {
		return nil, neuro.NewFailureResult("Cell " + params.Cell + " is not available")
	}

	return params.Cell, neuro.NewSuccessResult("Playing in cell " + params.Cell)
}

func (a *PlayAction) Execute(state interface{}) {
	cell := state.(string)
	a.game.PlayInCell(cell)
}

func main() {
	// Get websocket URL from environment or use default
	wsURL := os.Getenv("NEURO_SDK_WS_URL")
	if wsURL == "" {
		wsURL = "ws://localhost:8000"
	}

	// Create client
	client, err := neuro.NewClient(neuro.ClientConfig{
		Game:         "Example Game",
		WebsocketURL: wsURL,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Connect
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	log.Println("✅ Connected to Neuro!")

	// Send initial context
	client.SendContext("Game has started. You can greet the player or give them items.", false)

	// Example 1: Register simple actions
	if err := client.RegisterActions([]neuro.ActionHandler{
		&GreetAction{},
		NewGiveItemAction(),
	}); err != nil {
		log.Fatalf("Failed to register actions: %v", err)
	}

	log.Println("✅ Registered simple actions")

	// Example 2: Using action windows for turn-based gameplay
	game := NewTicTacToeGame(client)
	client.SendContext("Let's play Tic Tac Toe! You are O, I am X.", false)

	// Create action window for player's turn
	window := client.NewActionWindow()
	window.AddAction(&PlayAction{game: game})
	window.SetForce(
		"It's your turn. Pick a cell to place your O.",
		neuro.WithPriority(neuro.PriorityMedium),
		neuro.WithEphemeralContext(false),
	)

	if err := window.Register(); err != nil {
		log.Fatalf("Failed to register action window: %v", err)
	}

	log.Println("✅ Registered Tic Tac Toe action window")

	// Handle errors
	go func() {
		for err := range client.Errors() {
			log.Printf("❌ Error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	client.SendShutdownReady()
}
