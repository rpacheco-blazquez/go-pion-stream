package relay

import (
	"log"
	"sync"
)

// ConnectionManager manages all channels, clients, and streams.
type ConnectionManager struct {
	Channels map[string]*Channel
	Mutex    sync.Mutex
}

// NewConnectionManager creates and initializes a new ConnectionManager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		Channels: make(map[string]*Channel),
	}
}

// CreateChannel creates a new channel with the given code.

func (cm *ConnectionManager) CreateChannel(code string) {
	if _, exists := cm.ValidateChannel(code); !exists {
		cm.Mutex.Lock()
		cm.Channels[code] = &Channel{
			Code:    code,
			Clients: make(map[int]*Client),
			Streams: make(map[int]*Stream),
			manager: cm,
		}
		log.Printf("[relay] Canal creado: %s", code)
		cm.Mutex.Unlock()
	}
}

// RemoveChannel removes a channel and closes all associated clients.
func (cm *ConnectionManager) RemoveChannel(code string) {
	if channel, exists := cm.ValidateChannel(code); exists {
		channel.Mutex.Lock()
		for _, client := range channel.Clients {
			close(client.Done)
		}
		channel.Mutex.Unlock()

		cm.Mutex.Lock()
		delete(cm.Channels, code)
		cm.Mutex.Unlock()
	}
}

func (cm *ConnectionManager) BroadcastToStream(channelCode string, streamID int, frame []byte) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		channel.Mutex.Lock()
		if stream, exists := channel.streamExist(streamID); exists {
			stream.Data = frame
			for _, client := range channel.Clients {
				select {
				case client.Chan <- stream.Data:
				default:
				}
			}
		}
		channel.Mutex.Unlock()
	}
}

// BroadcastToClient es una función mínima para enviar un frame a todos los clientes de un canal.
func (cm *ConnectionManager) BroadcastToClient(channelCode string, frame []byte) {
	channel, exists := cm.ValidateChannel(channelCode)
	if !exists {
		return
	}
	channel.Mutex.Lock()
	defer channel.Mutex.Unlock()
	for _, client := range channel.Clients {
		select {
		case client.Chan <- frame:
		default:
		}
	}
}

// ValidateChannel checks if a channel exists and returns it.
func (cm *ConnectionManager) ValidateChannel(code string) (*Channel, bool) {
	cm.Mutex.Lock()
	defer cm.Mutex.Unlock()
	channel, exists := cm.Channels[code]
	return channel, exists
}

// ListChannels returns a list of all channel codes.
func (cm *ConnectionManager) ListAllChannels() []string {
	cm.Mutex.Lock()
	defer cm.Mutex.Unlock()
	var channelCodes []string
	for code := range cm.Channels {
		channelCodes = append(channelCodes, code)
	}
	return channelCodes
}

// ListClients returns a list of all clients across all channels.
func (cm *ConnectionManager) ListAllClients() []*Client {
	cm.Mutex.Lock()
	defer cm.Mutex.Unlock()
	var clients []*Client
	for _, channel := range cm.Channels {
		clients = append(clients, channel.ListClients()...)
	}
	return clients
}

// ListStreams returns a list of all streams across all channels.
func (cm *ConnectionManager) ListAllStreams() map[string]map[int]*Stream {
	cm.Mutex.Lock()
	defer cm.Mutex.Unlock()
	streams := make(map[string]map[int]*Stream)
	for code, channel := range cm.Channels {
		channelStreams := channel.ListStreams()
		if len(channelStreams) > 0 {
			streams[code] = channelStreams
		}
	}
	return streams
}
