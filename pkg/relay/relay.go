package relay

import (
	"log"
	"sync"
	"time"
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

var broadcastLogTicker = time.NewTicker(5 * time.Second)
var broadcastLogTicker2 = time.NewTicker(5 * time.Second)
var broadcastLogTicker3 = time.NewTicker(5 * time.Second)

func (cm *ConnectionManager) BroadcastToStream(channelCode string, streamID int, frame []byte) {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		select {
		case <-broadcastLogTicker.C:
			log.Printf("[BroadcastToStream] Canal validado: %s, ahora streamID: %d", channelCode, streamID)
		default:
		}

		channel.Mutex.Lock()
		if stream, exists := channel.streamExist(streamID); exists {
			select {
			case <-broadcastLogTicker2.C:
				log.Printf("[BroadcastToStream] Stream validado: %d en canal %s, frame: %v", streamID, channelCode, frame)
			default:
			}

			stream.Data = frame
			// Enviar el frame a todos los clientes del canal
			now := time.Now()
			for _, client := range channel.Clients {
				select {
				case client.Chan <- frame:
					if client.LastLog.IsZero() || now.Sub(client.LastLog) >= 5*time.Second {
						log.Printf("[BroadcastToStream] Frame enviado a clientID %d", client.ID)
						client.LastLog = now
					}
				default:
					if client.LastLog.IsZero() || now.Sub(client.LastLog) >= 5*time.Second {
						log.Printf("[BroadcastToStream] Canal lleno para clientID %d, frame descartado", client.ID)
						client.LastLog = now
					}
				}
			}

		} else {
			log.Printf("[BroadcastToStream] Stream con ID %d no encontrado en canal %s", streamID, channelCode)
		}
		channel.Mutex.Unlock()
	} else {
		select {
		case <-broadcastLogTicker3.C:
			log.Printf("[BroadcastToStream] Canal no encontrado: %s", channelCode)
		default:
		}
	}
}

// BroadcastToClient es una función mínima para enviar un frame a todos los clientes de un canal.
func (cm *ConnectionManager) BroadcastToClient(channelCode string, frame []byte) {
	now := time.Now()
	channel, exists := cm.ValidateChannel(channelCode)
	if !exists {
		return
	}
	channel.Mutex.Lock()
	defer channel.Mutex.Unlock()
	for _, client := range channel.Clients {
		select {
		case client.Chan <- frame:
			if client.LastLog.IsZero() || now.Sub(client.LastLog) >= 5*time.Second {
				log.Printf("[BroadcastToClient] Frame enviado a clientID %d", client.ID)
				client.LastLog = now
			}
			// Frame enviado correctamente
		default:
			if client.LastLog.IsZero() || now.Sub(client.LastLog) >= 5*time.Second {
				log.Printf("[BroadcastToClient] Canal lleno para clientID %d, frame descartado", client.ID)
				client.LastLog = now
			}
			// Canal lleno, frame descartado
		}
	}
}

// ValidateChannel checks if a channel exists and returns it.
func (cm *ConnectionManager) ValidateChannel(code string) (*Channel, bool) {
	// log.Printf("[ConnectionManager] ValidateChannel: Validando canal %s", code)
	cm.Mutex.Lock()
	defer func() {
		cm.Mutex.Unlock()
	}()
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
	log.Printf("[ConnectionManager] ListAllStreams: Listando todos los streams en todos los canales")
	cm.Mutex.Lock()
	defer cm.Mutex.Unlock()
	streams := make(map[string]map[int]*Stream)
	for code, channel := range cm.Channels {
		log.Printf("[ConnectionManager] ListAllStreams: Listando streams para el canal %s", code)
		channelStreams := channel.ListStreams()
		if len(channelStreams) > 0 {
			for streamID := range channelStreams {
				log.Printf("[ConnectionManager] ListAllStreams: Canal: %s, StreamID: %d", code, streamID)
			}
			streams[code] = channelStreams
		} else {
			log.Printf("[ConnectionManager] ListAllStreams: Canal %s no tiene streams activos, no se añadirá al mapa", code)
		}
	}
	log.Printf("[ConnectionManager] ListAllStreams: Total de canales con streams listados: %d", len(streams))
	return streams
}
