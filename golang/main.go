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

	// init
	msgStore := &sync.Map{}
	topoStore := &topology{
		Mutex:      &sync.Mutex{},
		Neighbours: make([]string, 0),
	}

	// defaulting to all the nodes as neighbours
	for _, neighbour := range n.NodeIDs() {
		if neighbour == n.ID() {
			continue
		}

		topoStore.Neighbours = append(topoStore.Neighbours, neighbour)
	}

	n.Handle("echo", echoHandler(n))
	n.Handle("generate", generateHandler(n))
	n.Handle("broadcast", broadcastHandler(n, topoStore, msgStore))
	n.Handle("broadcast_ok", func(msg maelstrom.Message) error { return nil })
	n.Handle("read", readHandler(n, msgStore))
	n.Handle("topology", topologyHandler(n, topoStore))

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

type topology struct {
	*sync.Mutex
	Neighbours []string
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

type broadcastMsg struct {
	Type    string `json:"type"`
	Message int    `json:"message"`
}

func broadcastHandler(n *maelstrom.Node, topoStore *topology, msgstore *sync.Map) maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		// Unmarshal the message body as an loosely-typed map.
		var payload broadcastMsg
		if err := json.Unmarshal(msg.Body, &payload); err != nil {
			return err
		}

		// Check first if the message is already in the store
		_, loaded := msgstore.LoadOrStore(payload.Message, struct{}{})
		if loaded {
			return nil
		}

		// broadcast the message to all the neighbours
		if err := broadcast(n, topoStore, &payload); err != nil {
			return err
		}

		// Echo the original message back with the updated message type.
		return n.Reply(msg, map[string]any{
			"type": "broadcast_ok",
		})
	}
}

func broadcast(n *maelstrom.Node, topoStore *topology, msg *broadcastMsg) error {
	topoStore.Lock()
	defer topoStore.Unlock()

	for _, neighbour := range topoStore.Neighbours {
		if err := n.Send(neighbour, msg); err != nil {
			return err
		}
	}

	return nil
}

func readHandler(n *maelstrom.Node, store *sync.Map) maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		currMsg := make([]int, 0)

		store.Range(
			func(key, value interface{}) bool {
				keyI, ok := key.(int)
				if !ok {
					return false
				}
				currMsg = append(currMsg, keyI)

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

func topologyHandler(n *maelstrom.Node, topoStore *topology) maelstrom.HandlerFunc {
	type topologyMsg struct {
		Type     string              `json:"type"`
		Topology map[string][]string `json:"topology"`
	}

	return func(msg maelstrom.Message) error {
		// Unmarshal the message body as an loosely-typed map.
		var payload topologyMsg
		if err := json.Unmarshal(msg.Body, &payload); err != nil {
			return err
		}

		neighbours, ok := payload.Topology[n.ID()]
		if ok {
			topoStore.Lock()
			topoStore.Neighbours = neighbours
			topoStore.Unlock()
		}

		return n.Reply(msg, map[string]any{
			"type": "topology_ok",
		})
	}
}
