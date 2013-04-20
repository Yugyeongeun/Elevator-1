package elevator

import (
	"bytes"
	"fmt"
	zmq "github.com/alecthomas/gozmq"
	configfile "github.com/msbranco/goconfig"
	"log"
)

type ClientSocket struct {
	Id     []byte
	Socket zmq.Socket
}

func server_socket(endpoint string) (*zmq.Socket, error) {
	context, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}

	socket, err := context.NewSocket(zmq.ROUTER)
	if err != nil {
		return nil, err
	}

	socket.Bind(endpoint)

	return &socket, nil
}

func request_handler(client_socket *ClientSocket, raw_msg []byte, db_store *DbStore) {
	request := new(Request)
	msg := bytes.NewBuffer(raw_msg)
	request.UnpackFrom(msg)
	request.Source = client_socket

	fmt.Println(request)
	if request.Db != "" {
		if db, ok := db_store.Container[request.Db]; ok {
			if db.Status == DB_STATUS_UNMOUNTED {
				db.Mount()
			}
			db.Channel <- request
		}
	} else {
		go store_commands[request.Command](db_store, request)
	}
}

func Runserver(config *configfile.ConfigFile) {
	endpoint, err := config.GetString("global", "endpoint")
	if err != nil {
		log.Fatal(err)
	}

	socket, err := server_socket("tcp://" + endpoint)
	defer (*socket).Close()
	if err != nil {
		log.Fatal(err)
	}


	// Bootstraping database store
	store_path, err := config.GetString("global", "database_store")
	storage_path, err := config.GetString("global", "databases_storage_path")
	default_db, err := config.GetString("global", "default_db")
	if err != nil {
		log.Fatal(err)
	}

	db_store := NewDbStore(store_path, storage_path)
	err = db_store.Add(default_db)
	if err != nil {
		log.Fatal(err)
	}

	poller := zmq.PollItems{
		zmq.PollItem{Socket: *socket, zmq.Events: zmq.POLLIN},
	}

	for i := 0; ; i++ {
		_, _ = zmq.Poll(poller, -1)

		switch {
		case poller[0].REvents&zmq.POLLIN != 0:
			parts, _ := poller[0].Socket.RecvMultipart(0)

			client_socket := ClientSocket{
				Id:     parts[0],
				Socket: *socket,
			}
			msg := parts[1]

			go request_handler(&client_socket, msg, db_store)
		}
	}
}
