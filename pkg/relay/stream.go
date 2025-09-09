package relay

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"log"
	"sync"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/pion/webrtc/v4"

	"embed"
)

//go:embed LiberationSans-Regular.ttf
var liberationSansTTF embed.FS

// Stream represents a data stream (e.g., video/audio).
type Stream struct {
	ID             int
	Data           []byte
	Running        bool
	Mutex          sync.Mutex
	PeerConnection *webrtc.PeerConnection
}

// Start begins the stream.
func (s *Stream) Start() error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.Running {
		return fmt.Errorf("stream %d is already running", s.ID)
	}
	s.Running = true
	log.Printf("[relay] Stream iniciado: streamID=%d", s.ID)
	return nil
}

// Stop halts the stream.
func (s *Stream) Stop() error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if !s.Running {
		return fmt.Errorf("stream %d is not running", s.ID)
	}
	s.Running = false
	log.Printf("[relay] Stream detenido: streamID=%d", s.ID)
	return nil
}

// AssociatePeerConnection associates a PeerConnection with the stream.
func (s *Stream) AssociatePeerConnection(pc *webrtc.PeerConnection) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.PeerConnection = pc
}

// GetPeerConnection retrieves the PeerConnection associated with the stream.
func (s *Stream) GetPeerConnection() *webrtc.PeerConnection {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	return s.PeerConnection
}

// RemovePeerConnection disassociates the PeerConnection from the stream.
func (s *Stream) RemovePeerConnection() {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.PeerConnection != nil {
		s.PeerConnection.Close() // Close the PeerConnection if it exists
		s.PeerConnection = nil
	}
}

// StartStreamForChannel starts the stream for a specific channel using ConnectionManager.
func (cm *ConnectionManager) StartStreamForChannel(channelCode string, streamID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.StartStream(streamID)
	}
	return fmt.Errorf("channel with code %s does not exist", channelCode)
}

// StopStreamForChannel stops the stream for a specific channel using ConnectionManager.
func (cm *ConnectionManager) StopStreamForChannel(channelCode string, streamID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.StopStream(streamID)
	}
	return fmt.Errorf("channel with code %s does not exist", channelCode)
}

// AssociateStreamPeerConnection associates a PeerConnection with a specific stream in the channel.
func (ch *Channel) AssociateStreamPeerConnection(streamID int, pc *webrtc.PeerConnection) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		stream.AssociatePeerConnection(pc)
		return nil
	}
	return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
}

// GetStreamPeerConnection retrieves the PeerConnection associated with a specific stream in the channel.
func (ch *Channel) GetStreamPeerConnection(streamID int) (*webrtc.PeerConnection, error) {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		return stream.GetPeerConnection(), nil
	}
	return nil, fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
}

// RemoveStreamPeerConnection disassociates the PeerConnection from a specific stream in the channel.
func (ch *Channel) RemoveStreamPeerConnection(streamID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		stream.RemovePeerConnection()
		return nil
	}
	return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
}

// StartStream associates the stream's data with the clients in the channel.
func (ch *Channel) StartStream(streamID int) error {
	ch.Mutex.Lock()
	stream, exists := ch.streamExist(streamID)
	if !exists {
		ch.Mutex.Unlock()
		return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
	}
	if err := stream.Start(); err != nil {
		ch.Mutex.Unlock()
		return fmt.Errorf("failed to start stream %d: %w", streamID, err)
	}
	ch.Mutex.Unlock()
	clients := ch.ListClients()
	stream.Mutex.Lock()
	frame := stream.Data
	stream.Mutex.Unlock()
	for _, client := range clients {
		select {
		case client.Chan <- frame:
		default:
		}
	}
	return nil
}

// StopStream stops the stream for a specific channel and ensures no data is sent to clients.
func (ch *Channel) StopStream(streamID int) error {
	ch.Mutex.Lock()
	stream, exists := ch.streamExist(streamID)
	if !exists {
		ch.Mutex.Unlock()
		return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
	}
	if err := stream.Stop(); err != nil {
		ch.Mutex.Unlock()
		return fmt.Errorf("failed to stop stream %d: %w", streamID, err)
	}
	stoppedImage := generateStoppedStreamImage("Stream Stopped")
	if stoppedImage == nil {
		ch.Mutex.Unlock()
		return fmt.Errorf("failed to generate stopped stream image")
	}
	ch.Mutex.Unlock()
	clients := ch.ListClients()
	go func(clients []*Client, stoppedImage []byte, ch *Channel) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			<-ticker.C
			ch.Mutex.Lock()
			active := ch.ActiveStreamID
			var running bool
			if active != nil {
				if stream, exists := ch.streamExist(*active); exists {
					stream.Mutex.Lock()
					running = stream.Running
					stream.Mutex.Unlock()
				}
			}
			ch.Mutex.Unlock()
			if running {
				return
			}
			for _, client := range clients {
				for {
					var empty = false
					select {
					case <-client.Chan:
					default:
						empty = true
					}
					if empty {
						break
					}
				}
				select {
				case client.Chan <- stoppedImage:
				default:
				}
			}
		}
	}(clients, stoppedImage, ch)
	if err := ch.RemoveStream(streamID); err != nil {
		// Only log critical error
		log.Printf("[Channel] Error removing stream %d: %v", streamID, err)
	}
	return nil
}

// generateStoppedStreamImage creates a black image with a custom message.
func generateStoppedStreamImage(message string) []byte {
	const width, height = 640, 480
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill the image with black color
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{black}, image.Point{}, draw.Src)

	// Load the font from embedded FS
	fontBytes, err := liberationSansTTF.ReadFile("LiberationSans-Regular.ttf")
	if err != nil {
		log.Printf("[Error] Embedded LiberationSans-Regular.ttf not found in binary: %v", err)
		return nil
	}
	font, err := truetype.Parse(fontBytes)
	if err != nil {
		log.Printf("[Error] Failed to parse embedded font: %v", err)
		return nil
	}

	// Create a freetype context
	c := freetype.NewContext()
	dpi := 72.0
	fontSize := float64(height) * 0.05 // Font size is 5% of the image height
	c.SetDPI(dpi)
	c.SetFont(font)
	c.SetFontSize(fontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.White)

	// Calculate text position to center it
	textWidth := len(message) * int(fontSize/2) // Approximation of text width
	x := (width - textWidth) / 2
	y := height/2 + int(fontSize/2) // Center vertically, adjust for baseline
	pt := freetype.Pt(x, y)

	// Draw the text
	_, err = c.DrawString(message, pt)
	if err != nil {
		log.Printf("[Error] Failed to draw text: %v", err)
		return nil
	}

	// Encode the image to JPEG
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, nil); err != nil {
		log.Printf("[Error] Failed to encode stopped stream image: %v", err)
		return nil
	}

	return buf.Bytes()
}
