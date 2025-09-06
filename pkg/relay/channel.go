package relay

import (
	"fmt"
	"sync"
)

// Channel represents a streaming channel with its clients and stream data.
type Channel struct {
	Code    string
	Clients map[int]*Client
	Mutex   sync.Mutex
	Streams map[int]*Stream // Map to manage multiple streams
}

// AddClient adds a client to the channel.
func (ch *Channel) AddClient(clientID int) (*Client, error) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if _, exists := ch.clientExist(clientID); exists {
		return nil, fmt.Errorf("client with ID %d already exists", clientID)
	}
	client := NewClient(clientID)
	ch.Clients[clientID] = client
	return client, nil
}

// RemoveClient removes a client from the channel.
func (ch *Channel) RemoveClient(clientID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if _, exists := ch.clientExist(clientID); !exists {
		return fmt.Errorf("client with ID %d does not exist", clientID)
	}
	delete(ch.Clients, clientID)
	return nil
}

// clientExist checks if a client exists in the channel by its ID.
func (ch *Channel) clientExist(clientID int) (*Client, bool) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	client, exists := ch.Clients[clientID]
	return client, exists
}

// GetClient retrieves a client from the channel by its ID.
func (ch *Channel) GetClient(clientID int) (*Client, error) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if client, exists := ch.clientExist(clientID); exists {
		return client, nil
	}
	return nil, fmt.Errorf("client with ID %d does not exist in channel %s", clientID, ch.Code)
}

// AddClient adds a client to a channel.
func (cm *ConnectionManager) AddClient(channelCode string, clientID int) *Client {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		if client, err := channel.AddClient(clientID); err == nil {
			return client
		}
	}
	return nil
}

// RemoveClient removes a client from a channel.
func (cm *ConnectionManager) RemoveClient(channelCode string, clientID int) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		_ = channel.RemoveClient(clientID)
	}
}

// ListClients returns a list of all clients in the channel.
func (cm *ConnectionManager) ListClients(channelCode string) []*Client {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.ListClients()
	}
	return nil
}

// ListClients returns a list of all clients in the channel.
func (ch *Channel) ListClients() []*Client {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	var clients []*Client
	for _, client := range ch.Clients {
		clients = append(clients, client)
	}
	return clients
}

// AttachStream associates a stream with the channel.
func (ch *Channel) AttachStream(streamID int, data []byte) (*Stream, error) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if _, exists := ch.streamExist(streamID); exists {
		return nil, fmt.Errorf("stream with ID %d already exists in channel %s", streamID, ch.Code)
	}
	stream := &Stream{
		ID:      streamID,
		Data:    data,
		Running: false,
	}
	ch.Streams[streamID] = stream
	return stream, nil
}

// RemoveStream removes a specific stream associated with the channel.
func (ch *Channel) RemoveStream(streamID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if _, exists := ch.streamExist(streamID); !exists {
		return fmt.Errorf("stream with ID %d does not exist in channel %s", streamID, ch.Code)
	}
	delete(ch.Streams, streamID)
	return nil
}

// GetStream retrieves a specific stream associated with the channel.
func (ch *Channel) GetStream(streamID int) (*Stream, error) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		return stream, nil
	}
	return nil, fmt.Errorf("stream with ID %d does not exist in channel %s", streamID, ch.Code)
}

// streamExist checks if a stream exists in the channel by its ID.
func (ch *Channel) streamExist(streamID int) (*Stream, bool) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	stream, exists := ch.Streams[streamID]
	return stream, exists
}

// AddStream associates a new stream with a channel.
func (cm *ConnectionManager) AddStream(channelCode string, stream *Stream) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		_, _ = channel.AttachStream(stream.ID, stream.Data)
	}
}

// RemoveStream removes the stream associated with a channel.
func (cm *ConnectionManager) RemoveStream(channelCode string, streamID int) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		_ = channel.RemoveStream(streamID)
	}
}

// ListStreams returns the stream associated with the channel.
func (ch *Channel) ListStreams() map[int]*Stream {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	return ch.Streams
}

// ListStreams returns the stream associated with the channel.
func (cm *ConnectionManager) ListStreams(channelCode string) map[int]*Stream {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.ListStreams()
	}
	return nil
}
