package main

import (
	"encoding/json"
	"log"
	"sync"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"

	"github.com/rs/xid"
)

func main() {
	n := maelstrom.NewNode()

	store := &sync.Map{}
	n.Handle("echo", echoHandler(n))
	n.Handle("generate", generateHandler(n))
	n.Handle("broadcast", broadcastHandler(n, store))
	n.Handle("read", readHandler(n, store))
	n.Handle("topology", topologyHandler(n))

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

func echoHandler(n *maelstrom.Node) maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		// Unmarshal the message body as an loosely-typed map.
		var body map[string]any
		if err := json.Unmarshal(msg.Body, &body); err != nil {
			return err
		}

		// Update the message type to return back.
		body["type"] = "echo_ok"

		// Echo the original message back with the updated message type.
		return n.Reply(msg, body)
	}
}

func generateHandler(n *maelstrom.Node) maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		// Echo the original message back with the updated message type.
		return n.Reply(msg, map[string]any{
			"type": "generate_ok",
			"id":   xid.New().String(),
		})
	}
}

func broadcastHandler(n *maelstrom.Node, store *sync.Map) maelstrom.HandlerFunc {
	type broadcastMsg struct {
		Type    string `json:"type"`
		Message int    `json:"message"`
	}

	return func(msg maelstrom.Message) error {
		// Unmarshal the message body as an loosely-typed map.
		var payload broadcastMsg
		if err := json.Unmarshal(msg.Body, &payload); err != nil {
			return err
		}

		store.Store(payload.Message, struct{}{})

		// Echo the original message back with the updated message type.
		return n.Reply(msg, map[string]any{
			"type": "broadcast_ok",
		})
	}
}

func readHandler(n *maelstrom.Node, store *sync.Map) maelstrom.HandlerFunc {
	type readMsg struct {
		Type string `json:"type"`
	}

	return func(msg maelstrom.Message) error {
		currMsg := make([]int, 0)
		store.Range(
			func(key, value interface{}) bool {
				currMsg = append(currMsg, key.(int))

				return true
			},
		)

		// Echo the original message back with the updated message type.
		return n.Reply(msg, map[string]any{
			"type":     "read_ok",
			"messages": currMsg,
		})
	}
}

func topologyHandler(n *maelstrom.Node) maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		return n.Reply(msg, map[string]any{
			"type": "topology_ok",
		})
	}
}
