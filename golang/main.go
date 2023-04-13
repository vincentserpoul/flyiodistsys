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

const retryTime = 10 * time.Millisecond

func main() {
	n := maelstrom.NewNode()

	// init
	msgStore := &sync.Map{}
	topoStore := &topology{
		Mutex:      &sync.Mutex{},
		Neighbours: make(map[string]*chan *broadcastMsg, 0),
	}

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

type topology struct {
	*sync.Mutex
	Neighbours map[string]*chan *broadcastMsg
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
	topoStore *topology,
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
	topoStore *topology,
	msg *broadcastMsg,
) {
	topoStore.Lock()
	defer topoStore.Unlock()

	for _, neighbour := range topoStore.Neighbours {
		*neighbour <- msg
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

		// neighbours, ok := payload.Topology[n.ID()]
		ok := true
		neighbours := n.NodeIDs()

		if ok {
			topoStore.Lock()

			for _, neighbour := range neighbours {
				if neighbour == n.ID() {
					continue
				}

				// if it already exists, skip
				if _, ok := topoStore.Neighbours[neighbour]; ok {
					continue
				}

				const maxMsgQ = 1000
				msgC := make(chan *broadcastMsg, maxMsgQ)

				// launch a goroutine to handle the broadcast
				go func(neighbour string) {
					log.Printf("launching go routine for node %s", neighbour)

					for {
						msg := <-msgC
						log.Printf("received message %d to send to %s", msg.Message, neighbour)

						const reqTimeout = 100 * time.Millisecond
						ctx, cancel := context.WithTimeout(context.Background(), reqTimeout)

						if _, err := n.SyncRPC(ctx, neighbour, msg); err != nil {
							cancel()
							log.Printf("error sending broadcast message %d to %s: %v", msg.Message, neighbour, err)
							// requeue msg
							msgC <- msg

							log.Printf("error sending broadcast message %d to %s: %v", msg.Message, neighbour, err)
							time.Sleep(retryTime)

							continue
						}

						log.Printf("successfully sent message %d to %s", msg.Message, neighbour)
					}
				}(neighbour)

				topoStore.Neighbours[neighbour] = &msgC
			}

			topoStore.Unlock()
		}

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
