package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
	"github.com/rs/xid"
)

// const retryTime = 5 * time.Millisecond

func main() {
	n := maelstrom.NewNode()

	// init
	msgStore := &sync.Map{}
	topoStore := make(map[string]*chan *broadcastMsg, 0)

	n.Handle("echo", echoHandler(n))
	n.Handle("generate", generateHandler(n))
	n.Handle("broadcast", broadcastHandler(n, topoStore, msgStore))
	n.Handle("broadcast_ok", broadcastOkHandler())
	n.Handle("read", readHandler(n, msgStore))
	n.Handle("topology", topologyHandler(n, topoStore))

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

type broadcastMsg struct {
	Type    string `json:"type"`
	Message int    `json:"message"`
}

func broadcastHandler(
	n *maelstrom.Node,
	topoStore map[string]*chan *broadcastMsg,
	msgstore *sync.Map,
) maelstrom.HandlerFunc {
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

		broadcast(topoStore, &payload)

		// Echo the original message back with the updated message type.
		return n.Reply(msg, map[string]any{
			"type": "broadcast_ok",
		})
	}
}

func broadcast(
	topoStore map[string]*chan *broadcastMsg,
	msg *broadcastMsg,
) {
	for _, neightbourChan := range topoStore {
		*neightbourChan <- msg
	}
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

func updateTopology(n *maelstrom.Node, topoStore map[string]*chan *broadcastMsg, neighbours []string) {
	for _, neighbour := range neighbours {
		if neighbour == n.ID() {
			continue
		}

		// if it already exists, skip
		if _, ok := topoStore[neighbour]; ok {
			continue
		}

		const maxMsgQ = 10000
		msgC := make(chan *broadcastMsg, maxMsgQ)

		// launch a goroutine to handle the broadcast
		go func(neighbour string) {
			for msg := range msgC {
				msg := msg
				go func() {
					const reqTimeout = 120 * time.Millisecond

					ctx, cancel := context.WithTimeout(context.Background(), reqTimeout)
					defer cancel()

					if _, err := n.SyncRPC(ctx, neighbour, msg); err != nil {
						// log.Printf("error sending broadcast message %d to %s: %v", msg.Message, neighbour, err)
						// requeue msg
						msgC <- msg
					}
				}()
			}
		}(neighbour)

		topoStore[neighbour] = &msgC
	}
}

func topologyHandler(n *maelstrom.Node, topoStore map[string]*chan *broadcastMsg) maelstrom.HandlerFunc {
	type topologyMsg struct {
		Type     string              `json:"type"`
		Topology map[string][]string `json:"topology"`
	}

	topoChan := make(chan *topologyMsg)

	go func() {
		for payload := range topoChan {
			neighbours, ok := payload.Topology[n.ID()]
			// for range topoChan {
			// 	ok := true
			// 	neighbours := n.NodeIDs()

			if ok {
				updateTopology(n, topoStore, neighbours)
			}
		}
	}()

	return func(msg maelstrom.Message) error {
		// Unmarshal the message body as an loosely-typed map.
		var payload topologyMsg
		if err := json.Unmarshal(msg.Body, &payload); err != nil {
			return err
		}

		topoChan <- &payload

		return n.Reply(msg, map[string]any{
			"type": "topology_ok",
		})
	}
}

func broadcastOkHandler() maelstrom.HandlerFunc {
	return func(msg maelstrom.Message) error {
		return nil
	}
}
