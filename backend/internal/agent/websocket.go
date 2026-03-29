package agent

import (
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
)

func handleServerMessage(msg *messages.ServerMessage) {
	switch p := msg.Payload.(type) {
	case *messages.ServerMessage_Pong:
		latency := time.Now().UnixMilli() - p.Pong.Timestamp
		Log.Printf("Pong received, latency: %dms", latency)
	case *messages.ServerMessage_Data:
		Log.Printf("Data received: %s = %s", p.Data.Key, p.Data.Value)
	case *messages.ServerMessage_Error:
		Log.Printf("Server error: %s", p.Error.Message)
	}
}

func connectWithRetry(url string) *websocket.Conn {
	for {
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			Log.Println("Connected to", url)
			return conn
		}
		Log.Printf("Connection failed, retrying in 5s: %v", err)
		time.Sleep(5 * time.Second)
	}
}
