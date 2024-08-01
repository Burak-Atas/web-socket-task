package server

// Message represents a message sent by a client
type Message struct {
	client  *Client
	Content []byte
	done    chan struct{}
}

func NewMessage(client *Client, msg []byte, done chan struct{}) *Message {
	return &Message{
		client:  client,
		Content: msg,
		done:    done,
	}
}
