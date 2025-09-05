package webrtc

import (
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
)

type Channel struct {
	StreamPeerConnection *webrtc.PeerConnection
	Viewers              map[string]*webrtc.PeerConnection
}

var (
	channels = make(map[string]*Channel)
	mu       sync.Mutex // Para proteger el acceso concurrente al mapa
)

// CreateChannel crea un nuevo canal con un código único
func CreateChannel(code string, pc *webrtc.PeerConnection) {
	mu.Lock()
	defer mu.Unlock()
	channels[code] = &Channel{
		StreamPeerConnection: pc,
		Viewers:              make(map[string]*webrtc.PeerConnection),
	}
}

// AddViewer añade un viewer a un canal existente
func AddViewer(code, viewerID string, pc *webrtc.PeerConnection) error {
	mu.Lock()
	defer mu.Unlock()
	channel, exists := channels[code]
	if !exists {
		return fmt.Errorf("channel %s does not exist", code)
	}
	channel.Viewers[viewerID] = pc
	return nil
}

// RemoveViewer elimina un viewer de un canal
func RemoveViewer(code, viewerID string) {
	mu.Lock()
	defer mu.Unlock()
	if channel, exists := channels[code]; exists {
		if pc, viewerExists := channel.Viewers[viewerID]; viewerExists {
			pc.Close()
			delete(channel.Viewers, viewerID)
		}
	}
}

// RemoveChannel elimina un canal y cierra todas las conexiones asociadas
func RemoveChannel(code string) {
	mu.Lock()
	defer mu.Unlock()
	if channel, exists := channels[code]; exists {
		if channel.StreamPeerConnection != nil {
			channel.StreamPeerConnection.Close()
		}
		for _, pc := range channel.Viewers {
			pc.Close()
		}
		delete(channels, code)
	}
}

func Relay(input, output string) {
	fmt.Printf("Relaying stream from %s to %s\n", input, output)
	// Aquí iría la lógica de relay entre protocolos
}
