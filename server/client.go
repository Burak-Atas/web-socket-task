package server

import "github.com/gorilla/websocket"

type Client struct {
	ID   string
	Conn *websocket.Conn
}

func NewClient(c *websocket.Conn, id string) *Client {
	return &Client{
		ID:   id,
		Conn: c,
	}
}
