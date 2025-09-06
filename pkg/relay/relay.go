package relay

import (
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
		}
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

// BroadcastFrame sends a frame to all clients in a channel.
func (cm *ConnectionManager) BroadcastFrame(channelCode string, frame []byte) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		channel.Mutex.Lock()
		for _, stream := range channel.Streams {
			stream.Data = frame
		}
		for _, client := range channel.Clients {
			select {
			case client.Chan <- frame:
			default:
			}
		}
		channel.Mutex.Unlock()
	}
}

// BroadcastToAll sends a frame to all clients across all channels.
func (cm *ConnectionManager) BroadcastToAll(frame []byte) {
	cm.Mutex.Lock()
	for channelCode, streams := range cm.ListAllStreams() {
		for _, stream := range streams {
			stream.Mutex.Lock()
			stream.Data = frame
			stream.Mutex.Unlock()
		}
		if channel, exists := cm.ValidateChannel(channelCode); exists {
			channel.Mutex.Lock()
			for _, client := range channel.Clients {
				select {
				case client.Chan <- frame:
				default:
				}
			}
			channel.Mutex.Unlock()
		}
	}
	cm.Mutex.Unlock()
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
		streams[code] = channel.ListStreams()
	}
	return streams
}
