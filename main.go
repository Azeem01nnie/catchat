package main

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------- WebSocket Upgrader ----------------------
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ---------------------- Client & Hub Structs ----------------------

type Client struct {
	conn      *websocket.Conn
	send      chan Message
	partner   *Client
	hub       *Hub
	tag       string
	mu        sync.Mutex
	createdAt time.Time
}

type Message struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type Hub struct {
	clients map[*Client]bool
	waiting map[string]*Client
	mu      sync.Mutex
}

// ---------------------- Hub Functions ----------------------

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
		waiting: make(map[string]*Client),
	}
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	if waitingClient, ok := h.waiting[c.tag]; ok && waitingClient == c {
		delete(h.waiting, c.tag)
	}
	h.mu.Unlock()
}

func (h *Hub) tryPair(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tag := c.tag
	if w, ok := h.waiting[tag]; ok && w != c {
		c.partner = w
		w.partner = c
		delete(h.waiting, tag)
		c.sendMessage("paired", "Paired with a partner in CatChat 🐱. Say hi!")
		w.sendMessage("paired", "Paired with a partner in CatChat 🐱. Say hi!")
	} else {
		h.waiting[tag] = c
		c.sendMessage("waiting", "Waiting for a partner with tag: "+tag+" in CatChat 🐱")
	}
}

// ---------------------- Client Functions ----------------------

func (c *Client) sendMessage(msgType, text string) {
	timestamp := time.Now().Format("15:04")
	c.send <- Message{
		Type:      msgType,
		Text:      text,
		Timestamp: timestamp,
	}
}

func (c *Client) readPump() {
	defer c.close()

	for {
		var msg Message
		if err := c.conn.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "message":
			text := filterMessage(msg.Text)
			c.mu.Lock()
			if c.partner != nil {
				c.partner.send <- Message{
					Type:      "message",
					Text:      text,
					Timestamp: time.Now().Format("15:04"),
				}
			} else {
				c.sendMessage("system", "No partner connected yet in CatChat 🐱.")
			}
			c.mu.Unlock()

		case "next":
			c.nextPartner()

		case "typing":
			c.mu.Lock()
			if c.partner != nil {
				c.partner.send <- Message{
					Type:      "typing",
					Text:      "Partner is typing...",
					Timestamp: time.Now().Format("15:04"),
				}
			}
			c.mu.Unlock()

		case "report":
			c.sendMessage("system", "Thank you. Report logged (demo).")
		}
	}
}

func (c *Client) writePump() {
	defer c.close()
	for msg := range c.send {
		if err := c.conn.WriteJSON(msg); err != nil {
			return
		}
	}
}

func (c *Client) nextPartner() {
	c.mu.Lock()
	if c.partner != nil {
		c.partner.sendMessage("partner_left", "Partner pressed Next. You are now looking for a new partner in CatChat 🐱.")
		c.partner.partner = nil
		c.partner = nil
	}
	c.mu.Unlock()
	hub.tryPair(c)
}

func (c *Client) close() {
	c.nextPartner()
	hub.removeClient(c)
	c.conn.Close()
	close(c.send)
}

// ---------------------- Profanity Filter ----------------------
var blockedWords = []string{"badword", "swear", "blocked"}

func filterMessage(msg string) string {
	lower := strings.ToLower(msg)
	for _, word := range blockedWords {
		if strings.Contains(lower, word) {
			msg = strings.ReplaceAll(msg, word, "****")
		}
	}
	return msg
}

// ---------------------- Main ----------------------
var hub = NewHub()

func main() {
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/ws", handleWS)

	addr := ":8080"
	log.Printf("CatChat server started at http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}

	tag := r.URL.Query().Get("tag")
	if tag == "" {
		tag = "default"
	}

	client := &Client{
		conn:      conn,
		send:      make(chan Message, 16),
		hub:       hub,
		tag:       tag,
		createdAt: time.Now(),
	}

	hub.addClient(client)
	go client.writePump()
	go client.readPump()
	hub.tryPair(client)
}
