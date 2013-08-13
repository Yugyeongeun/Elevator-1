package elevator

import (
	"bytes"
	"errors"
	"fmt"
	zmq "github.com/alecthomas/gozmq"
	l4g "github.com/alecthomas/log4go"
	"log"
)

type ClientSocket struct {
	Id     []byte
	Socket zmq.Socket
}

// Creates and binds the zmq socket for the server
// to listen on
func buildServerSocket(endpoint string) (*zmq.Socket, error) {
	context, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}

	socket, err := context.NewSocket(zmq.ROUTER)
	if err != nil {
		return nil, err
	}

	socket.Bind(endpoint)

	return socket, nil
}

// handleRequest deserializes the input msgpack request,
// processes it and ensures it is forwarded to the client.
func handleRequest(clientSocket *ClientSocket, rawMsg []byte, dbStore *DbStore) {
	var request *Request = new(Request)
	var msg *bytes.Buffer = bytes.NewBuffer(rawMsg)

	// Deserialize request message and fulfill request
	// obj with it's content
	request.UnpackFrom(msg)
	request.Source = clientSocket
	l4g.Debug(func() string { return request.String() })

	if request.DbUid != "" {
		if db, ok := dbStore.Container[request.DbUid]; ok {
			if db.Status == DB_STATUS_UNMOUNTED {
				db.Mount()
			}
			db.Channel <- request
		}
	} else {
		go func() {
			response, err := store_commands[request.Command](dbStore, request)
			if err == nil {
				forwardResponse(response, request)
			}
		}()
	}
}

// processRequest executes the received request command, and returns
// the resulting response.
func processRequest(db *Db, request *Request) (*Response, error) {
	if f, ok := database_commands[request.Command]; ok {
		response, _ := f(db, request)
		return response, nil
	}
	error := errors.New(fmt.Sprintf("Unknown command %s", request.Command))
	l4g.Error(error)

	return nil, error
}

// forwardResponse takes a request-response pair as input and
// sends the response to the request client.
func forwardResponse(response *Response, request *Request) error {
	l4g.Debug(func() string { return response.String() })

	var responseBuf bytes.Buffer
	var socket *zmq.Socket = &request.Source.Socket
	var address []byte = request.Source.Id
	var parts [][]byte = make([][]byte, 2)

	response.PackInto(&responseBuf)
	parts[0] = address
	parts[1] = responseBuf.Bytes()

	err := socket.SendMultipart(parts, 0)
	if err != nil {
		return err
	}

	return nil
}

func ListenAndServe(config *Config) error {
	l4g.Info(fmt.Sprintf("Elevator started on %s", config.Core.Endpoint))

	// Build server zmq socket
	socket, err := buildServerSocket(config.Core.Endpoint)
	defer (*socket).Close()
	if err != nil {
		log.Fatal(err)
	}

	// Load database store
	dbStore := NewDbStore(config)
	err = dbStore.Load()
	if err != nil {
		err = dbStore.Add("default")
		if err != nil {
			log.Fatal(err)
		}
	}

	// build zmq poller
	poller := zmq.PollItems{
		zmq.PollItem{Socket: socket, Events: zmq.POLLIN},
	}

	// Poll for events on the zmq socket
	// and handle the incoming requests in a goroutine
	for {
		_, _ = zmq.Poll(poller, -1)

		switch {
		case poller[0].REvents&zmq.POLLIN != 0:
			parts, _ := poller[0].Socket.RecvMultipart(0)

			clientSocket := ClientSocket{
				Id:     parts[0],
				Socket: *socket,
			}
			msg := parts[1]

			go handleRequest(&clientSocket, msg, dbStore)
		}
	}
}
