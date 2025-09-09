package relay

import (
	"fmt"
	"log"
	"sync"
)

// Channel represents a streaming channel with its clients and stream data.
type Channel struct {
	Code    string
	Clients map[int]*Client
	Mutex   sync.Mutex
	Streams map[int]*Stream // Map to manage multiple streams
	// Control de stream activo y pipeline MJPEG
	ActiveStreamID    *int
	FFmpegMJPEGActive bool
	ffmpegMJPEGCancel func()             // función de cancelación del pipeline MJPEG
	manager           *ConnectionManager // referencia al padre
}

// SetFFmpegMJPEGCancel guarda la función de cancelación del pipeline MJPEG
func (ch *Channel) SetFFmpegMJPEGCancel(cancel func()) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	ch.ffmpegMJPEGCancel = cancel
}

// CancelFFmpegMJPEG cancela el pipeline MJPEG si hay una función guardada
func (ch *Channel) CancelFFmpegMJPEG() {
	ch.Mutex.Lock()
	cancel := ch.ffmpegMJPEGCancel
	ch.ffmpegMJPEGCancel = nil
	ch.Mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Set el stream activo (sin mutex, debe llamarse con el lock ya tomado)
func (ch *Channel) SetActiveStreamID(streamID int) {
	ch.ActiveStreamID = &streamID
}

// Limpia el stream activo (sin mutex, debe llamarse con el lock ya tomado)
func (ch *Channel) ClearActiveStreamID() {
	ch.ActiveStreamID = nil
	// Si hay más streams, asignar el siguiente streamID disponible
	for id := range ch.Streams {
		ch.ActiveStreamID = &id
		break // solo el primero encontrado
	}
}

// Obtiene el stream activo
func (ch *Channel) GetActiveStreamID() *int {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	return ch.ActiveStreamID
}

// Controla si el pipeline MJPEG está activo
func (ch *Channel) IsFFmpegMJPEGActive() bool {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	return ch.FFmpegMJPEGActive
}

func (ch *Channel) SetFFmpegMJPEGActive(active bool) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	ch.FFmpegMJPEGActive = active
}

// AddClient adds a client to the channel.

func (ch *Channel) AddClient(clientID int) (*Client, error) {
	if _, exists := ch.clientExist(clientID); exists {
		return nil, fmt.Errorf("client with ID %d already exists", clientID)
	}
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	client := NewClient(clientID)
	_ = client.Connect() // Inicializa el estado de conexión
	ch.Clients[clientID] = client
	log.Printf("[relay] Cliente conectado: clientID=%d canal=%s", clientID, ch.Code)
	return client, nil
}

// RemoveClient removes a client from the channel.
func (ch *Channel) RemoveClient(clientID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	client, exists := ch.clientExist(clientID)
	if !exists {
		return fmt.Errorf("client with ID %d does not exist", clientID)
	}
	_ = client.Disconnect() // Cierra Done
	delete(ch.Clients, clientID)
	log.Printf("[relay] Cliente desconectado: clientID=%d canal=%s", clientID, ch.Code)
	// Verificar si el canal debe eliminarse
	go ch.ChannelNeedToBeRemoved()
	return nil
}

// clientExist checks if a client exists in the channel by its ID.
func (ch *Channel) clientExist(clientID int) (*Client, bool) {
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
		if client, _ := channel.AddClient(clientID); client != nil {
			return client
		}
	}
	return nil
}

// RemoveClient removes a client from a channel.
func (cm *ConnectionManager) RemoveClient(channelCode string, clientID int) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		err := channel.RemoveClient(clientID)
		if err == nil {
			log.Printf("[relay] Cliente desconectado (ConnectionManager): clientID=%d canal=%s", clientID, channelCode)
		}
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
	clients := make([]*Client, 0, len(ch.Clients))
	for _, client := range ch.Clients {
		clients = append(clients, client)
	}
	return clients
}

// AttachStream associates a stream with the channel.
func (ch *Channel) AttachStream(streamID int) (*Stream, error) {
	if _, exists := ch.streamExist(streamID); exists {
		return nil, fmt.Errorf("stream with ID %d already exists in channel %s", streamID, ch.Code)
	}
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	initialFrame := generateStoppedStreamImage("Esperando video...")
	stream := &Stream{
		ID:      streamID,
		Data:    initialFrame,
		Running: false,
	}
	ch.Streams[streamID] = stream
	if ch.ActiveStreamID == nil {
		ch.SetActiveStreamID(streamID)
	}
	log.Printf("[relay] Stream creado: streamID=%d canal=%s", streamID, ch.Code)
	return stream, nil
}

// RemoveStream removes a specific stream associated with the channel.
func (ch *Channel) RemoveStream(streamID int) error {
	ch.CancelFFmpegMJPEG()
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	stream, exists := ch.streamExist(streamID)
	if !exists {
		return fmt.Errorf("stream with ID %d does not exist in channel %s", streamID, ch.Code)
	}
	stream.RemovePeerConnection()
	delete(ch.Streams, streamID)
	if ch.ActiveStreamID != nil && *ch.ActiveStreamID == streamID {
		ch.ClearActiveStreamID()
	}
	log.Printf("[relay] Stream eliminado: streamID=%d canal=%s", streamID, ch.Code)
	// Verificar si el canal debe eliminarse
	go ch.ChannelNeedToBeRemoved()
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
	// log.Printf("[StreamExist] Verificando existencia de stream con ID %d", streamID)
	stream, exists := ch.Streams[streamID]
	// log.Printf("[StreamExist] Saliendo de verificar stream con ID %d: existe=%v", streamID, exists)
	return stream, exists
}

// AddStream associates a new stream with a channel.
func (cm *ConnectionManager) AddStream(channelCode string, streamID int) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		_, _ = channel.AttachStream(streamID)
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

// ChannelNeedToBeRemoved verifica si el canal debe eliminarse (sin clients ni streams) y lo elimina orgánicamente.
func (ch *Channel) ChannelNeedToBeRemoved() {
	ch.Mutex.Lock()
	clientsEmpty := len(ch.Clients) == 0
	streamsEmpty := len(ch.Streams) == 0
	ch.Mutex.Unlock()
	if clientsEmpty && streamsEmpty && ch.manager != nil {
		log.Printf("[relay] Canal eliminado orgánicamente: %s", ch.Code)
		ch.manager.RemoveChannel(ch.Code)
	}
}
