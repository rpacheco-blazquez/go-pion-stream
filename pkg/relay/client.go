package relay

import (
	"fmt"
	"sync"
	"time"
)

// Client represents a viewer connected to a channel.
type Client struct {
	ID        int
	Chan      chan []byte
	Done      chan struct{}
	IP        string                 // Dirección IP del cliente
	Connected time.Time              // Timestamp de conexión
	LastFrame time.Time              // Timestamp del último frame enviado
	LastLog   time.Time              // Timestamp del último log.Printf de envío/descartado
	Mutex     sync.Mutex             // Para manejar concurrencia en campos adicionales
	Metadata  map[string]interface{} // Información adicional (extensible)
}

// NewClient creates and initializes a new Client.
func NewClient(id int) *Client {
	return &Client{
		ID:   id,
		Chan: make(chan []byte, 5),
		Done: make(chan struct{}),
	}
}

// Connect establishes the client's connection.
func (c *Client) Connect() error {
	select {
	case <-c.Done:
		return fmt.Errorf("client %d is already disconnected", c.ID)
	default:
		return nil
	}
}

// Disconnect closes the client's connection.
func (c *Client) Disconnect() error {
	select {
	case <-c.Done:
		return fmt.Errorf("client %d is already disconnected", c.ID)
	default:
		close(c.Done)
		return nil
	}
}

// ConnectClient establishes the connection for a client in a specific channel.
func (cm *ConnectionManager) ConnectClient(channelCode string, clientID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.ConnectClient(clientID)
	}
	return fmt.Errorf("channel with code %s or client with ID %d does not exist", channelCode, clientID)
}

// DisconnectClient closes the connection for a client in a specific channel.
func (cm *ConnectionManager) DisconnectClient(channelCode string, clientID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.DisconnectClient(clientID)
	}
	return fmt.Errorf("channel with code %s or client with ID %d does not exist", channelCode, clientID)
}

// ConnectClient establishes the connection for a client in the channel.
func (ch *Channel) ConnectClient(clientID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if client, exists := ch.clientExist(clientID); exists {
		return client.Connect()
	}
	return fmt.Errorf("client with ID %d does not exist in channel %s", clientID, ch.Code)
}

// DisconnectClient closes the connection for a client in the channel.
func (ch *Channel) DisconnectClient(clientID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if client, exists := ch.clientExist(clientID); exists {
		return client.Disconnect()
	}
	return fmt.Errorf("client with ID %d does not exist in channel %s", clientID, ch.Code)
}
