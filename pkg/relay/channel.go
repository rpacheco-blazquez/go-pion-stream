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
}

// AddClient adds a client to the channel.
func (ch *Channel) AddClient(clientID int) (*Client, error) {
	log.Printf("[Channel] AddClient: Verificando si el cliente con ID %d ya existe en canal %s", clientID, ch.Code)
	if _, exists := ch.clientExist(clientID); exists {
		log.Printf("[Channel] AddClient: Cliente con ID %d ya existe en canal %s", clientID, ch.Code)
		return nil, fmt.Errorf("client with ID %d already exists", clientID)
	}

	log.Printf("[Channel] AddClient: Intentando bloquear mutex para canal %s", ch.Code)
	ch.Mutex.Lock()
	log.Printf("[Channel] AddClient: Mutex bloqueado para canal %s", ch.Code)
	defer func() {
		log.Printf("[Channel] AddClient: Liberando mutex para canal %s", ch.Code)
		ch.Mutex.Unlock()
	}()

	log.Printf("[Channel] AddClient: Creando nuevo cliente con ID %d en canal %s", clientID, ch.Code)
	client := NewClient(clientID)
	ch.Clients[clientID] = client
	log.Printf("[Channel] AddClient: Cliente con ID %d añadido exitosamente en canal %s", clientID, ch.Code)
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
	log.Printf("[Channel] clientExist: Verificando existencia de cliente %d en canal %s", clientID, ch.Code)

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
	log.Printf("[ConnectionManager] AddClient: Añadiendo cliente %d al canal %s", clientID, channelCode)
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		if client, err := channel.AddClient(clientID); err == nil {
			log.Printf("[ConnectionManager] AddClient: Cliente %d añadido al canal %s", clientID, channelCode)
			return client
		} else {
			log.Printf("[ConnectionManager] AddClient: Error añadiendo cliente %d al canal %s: %v", clientID, channelCode, err)
		}
	} else {
		log.Printf("[ConnectionManager] AddClient: Canal %s no existe", channelCode)
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
		log.Printf("[ConnectionManager] Validando canal %s: existe=%v", channelCode, exists)
		return channel.ListClients()
	}
	return nil
}

// ListClients returns a list of all clients in the channel.
func (ch *Channel) ListClients() []*Client {
	log.Printf("[Channel] ListClients: Listando clientes en canal %s", ch.Code)
	ch.Mutex.Lock()
	log.Printf("[Channel] ListClients: Mutex bloqueado para canal %s", ch.Code)
	defer func() {
		ch.Mutex.Unlock()
		log.Printf("[Channel] ListClients: Mutex liberado para canal %s", ch.Code)
	}()

	clients := make([]*Client, 0, len(ch.Clients))
	for _, client := range ch.Clients {
		clients = append(clients, client)
	}
	log.Printf("[Channel] ListClients: Clientes listados exitosamente en canal %s", ch.Code)
	return clients
}

// AttachStream associates a stream with the channel.
func (ch *Channel) AttachStream(streamID int) (*Stream, error) {
	log.Printf("[Channel] AttachStream: Intentando asociar stream con ID %d al canal %s", streamID, ch.Code)
	if _, exists := ch.streamExist(streamID); exists {
		log.Printf("[Channel] AttachStream: Stream con ID %d ya existe en el canal %s", streamID, ch.Code)
		return nil, fmt.Errorf("stream with ID %d already exists in channel %s", streamID, ch.Code)
	}

	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()

	// Inicializar el frame con una imagen negra con texto
	initialFrame := generateStoppedStreamImage("Esperando video...")
	stream := &Stream{
		ID:      streamID,
		Data:    initialFrame,
		Running: false,
	}
	ch.Streams[streamID] = stream
	log.Printf("[Channel] AttachStream: Stream con ID %d asociado exitosamente al canal %s", streamID, ch.Code)
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
	// log.Printf("[StreamExist] Verificando existencia de stream con ID %d", streamID)
	stream, exists := ch.Streams[streamID]
	// log.Printf("[StreamExist] Saliendo de verificar stream con ID %d: existe=%v", streamID, exists)
	return stream, exists
}

// AddStream associates a new stream with a channel.
func (cm *ConnectionManager) AddStream(channelCode string, streamID int) {
	log.Printf("[ConnectionManager] AddStream: Intentando añadir stream con ID %d al canal %s", streamID, channelCode)
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		log.Printf("[ConnectionManager] AddStream: Canal %s validado. Asociando stream...", channelCode)
		_, err := channel.AttachStream(streamID)
		if err != nil {
			log.Printf("[ConnectionManager] AddStream: Error asociando stream con ID %d al canal %s: %v", streamID, channelCode, err)
		} else {
			log.Printf("[ConnectionManager] AddStream: Stream con ID %d añadido exitosamente al canal %s", streamID, channelCode)
		}
	} else {
		log.Printf("[ConnectionManager] AddStream: Canal %s no encontrado. No se pudo añadir el stream con ID %d", channelCode, streamID)
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
	log.Printf("[Channel] ListStreams: Listando streams en el canal %s", ch.Code)
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	log.Printf("[Channel] ListStreams: Número de streams en el canal %s: %d", ch.Code, len(ch.Streams))
	for streamID := range ch.Streams {
		log.Printf("[Channel] ListStreams: Canal: %s, StreamID: %d", ch.Code, streamID)
	}
	return ch.Streams
}

// ListStreams returns the stream associated with the channel.
func (cm *ConnectionManager) ListStreams(channelCode string) map[int]*Stream {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.ListStreams()
	}
	return nil
}
