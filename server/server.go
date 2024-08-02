package server

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// time to read the next client's pong message
	pongWait = 60 * time.Second
	// time period to send pings to client
	pingPeriod = (pongWait * 9) / 10
	// time allowed to write a message to client
	writeWait = 10 * time.Second
	// max message size allowed
	maxMessageSize = 512
	// I/O read buffer size
	readBufferSize = 1024
	// I/O write buffer size
	writeBufferSize = 1024
)

// http to websocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  readBufferSize,
	WriteBufferSize: writeBufferSize,
	CheckOrigin: func(r *http.Request) bool {
		// allow all origin
		return true
	},
}

type Server struct {
	Clients    map[string]*Client
	Broadcast  chan Message
	Register   chan *Client
	Unregister chan *Client
	Mutex      sync.Mutex
	l          *log.Logger
}

func NewServer(l *log.Logger) *Server {
	return &Server{
		Clients:    make(map[string]*Client),
		Broadcast:  make(chan Message),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		l:          l,
	}
}

func (s *Server) Run() {
	for {
		select {
		case client := <-s.Register:
			s.JoinRegister(client)
		case client := <-s.Unregister:
			s.RemoveRegister(client)
		case message := <-s.Broadcast:
			s.Publish(message)
		}
	}
}

func (s *Server) HandleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}

	client := NewClient(ws, s.nextID())

	s.Register <- client

	defer func() {
		s.Unregister <- client
	}()

	// create channel to signal client health
	done := make(chan struct{})
	go s.Write(client, done)
	s.Read(client, done)
}
func (s *Server) nextID() string {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	return uuid.New().String()
}

func (s *Server) JoinRegister(client *Client) {
	s.Mutex.Lock()
	// if client already subbed, stop the process
	if _, subbed := s.Clients[client.ID]; subbed {
		return
	}

	s.Clients[client.ID] = client
	s.Mutex.Unlock()
	s.l.Printf("Client %s connected\n", client.ID)
}

func (s *Server) RemoveRegister(client *Client) {
	s.Mutex.Lock()
	if _, ok := s.Clients[client.ID]; ok {
		delete(s.Clients, client.ID)
		client.Conn.Close()
		s.l.Printf("Client %s disconnected\n", client.ID)
	}
	s.Mutex.Unlock()
}

// writePump sends ping to the client
func (s *Server) Write(client *Client, done <-chan struct{}) {
	// create ping ticker
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// send ping message
			err := client.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			if err != nil {
				// if error sending ping, remove this client from the server
				s.RemoveRegister(client)
				// stop sending ping
				return
			}
		case <-done:
			// if process is done, stop sending ping
			return
		}
	}
}

// readPump process incoming messages and set the settings
func (s *Server) Read(client *Client, done chan<- struct{}) {
	// set limit, deadline to read & pong handler
	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// message handling
	for {
		// read incoming message
		_, msg, err := client.Conn.ReadMessage()
		// if error occured
		if err != nil {
			// remove from the client
			s.RemoveRegister(client)
			// set health status to unhealthy by closing channel
			close(done)
			// stop process
			break
		}

		// if no error, process incoming message
		mess := Message{
			client:  client,
			Content: msg,
			done:    make(chan struct{}),
		}

		s.l.Println(mess.Content)

		s.Broadcast <- mess
	}
}
func (s *Server) Publish(message Message) {
	// Eğer istemci var ise mesajı diğer istemcilere gönder
	if _, exist := s.Clients[message.client.ID]; !exist {
		return
	}

	// Diğer istemcilere mesaj gönderme
	var wg sync.WaitGroup
	for _, conn := range s.Clients {
		if conn.ID != message.client.ID {
			wg.Add(1)
			go func(c *Client) {
				defer wg.Done()
				err := s.SendWithWait(c.Conn, string(message.Content))
				if err != nil {
					s.l.Printf("Failed to send message to client %s: %v", c.ID, err)
					c.Error(err)
				}
			}(conn)
		}
	}
	wg.Wait()
}

func (s *Server) SendWithWait(conn *websocket.Conn, message string) error {
	err := conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		return err
	}
	return nil
}
