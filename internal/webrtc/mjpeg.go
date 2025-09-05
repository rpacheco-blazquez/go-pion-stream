package webrtc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	clients             = map[int]*mjpegClient{}
	clientsMtx          sync.Mutex
	channelClients      = make(map[string]map[int]*Client)
	channelClientsMtx   sync.Mutex
	lastFrameForChannel = make(map[string][]byte)
	lastLogTime         = make(map[string]time.Time) // Map para rastrear el último tiempo de log por canal
)

type mjpegClient struct {
	id   int
	ch   chan []byte
	done chan struct{}
}

func makeBlackJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75})
	return buf.Bytes()
}

func watchHandler(w http.ResponseWriter, r *http.Request, code string) {
	log.Printf("[WatchHandler] Viewer intentando conectarse al canal %s", code)
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	client := &Client{ch: make(chan []byte, 1), done: make(chan struct{})}
	channelClientsMtx.Lock()
	if _, exists := channelClients[code]; !exists {
		channelClients[code] = make(map[int]*Client)
	}
	clientID := len(channelClients[code]) + 1
	channelClients[code][clientID] = client
	log.Printf("[WatchHandler] Cliente %d registrado en canal %s", clientID, code) // Log para verificar registro

	// Enviar el último frame al nuevo cliente si existe
	if lastFrame, ok := lastFrameForChannel[code]; ok {
		log.Printf("[WatchHandler] Enviando último frame al cliente %d en canal %s", clientID, code)
		client.ch <- lastFrame
	}
	channelClientsMtx.Unlock()

	defer func() {
		channelClientsMtx.Lock()
		delete(channelClients[code], clientID)
		if len(channelClients[code]) == 0 {
			delete(channelClients, code)
		}
		channelClientsMtx.Unlock()
		log.Printf("[WatchHandler] Cliente %d eliminado del canal %s", clientID, code) // Log para verificar eliminación
	}()

	log.Printf("[WatchHandler] Viewer conectado exitosamente al canal %s", code)
	log.Printf("[WatchHandler] Enviando frames al viewer en canal %s", code)
	for {
		select {
		case frame := <-client.ch:
			_, _ = w.Write([]byte("--frame\r\n"))
			_, _ = w.Write([]byte("Content-Type: image/jpeg\r\n\r\n"))
			_, _ = w.Write(frame)
			_, _ = w.Write([]byte("\r\n"))
			flusher.Flush()
		case <-client.done:
			return
		}
	}
}

func broadcastFrameToChannel(frame []byte, channel string) {
	currentTime := time.Now()
	channelClientsMtx.Lock()
	lastFrameForChannel[channel] = frame // Save the last frame for the channel
	if clients, ok := channelClients[channel]; ok {
		if lastLog, exists := lastLogTime[channel]; !exists || currentTime.Sub(lastLog) > 5*time.Second {
			log.Printf("[Broadcast] Canal %s tiene %d clientes registrados", channel, len(clients))
			lastLogTime[channel] = currentTime
		}
		for id, c := range clients {
			select {
			case c.ch <- frame:
				// Enviar frame sin loggear cada cliente
			default:
				if lastLog, exists := lastLogTime[channel]; !exists || currentTime.Sub(lastLog) > 5*time.Second {
					log.Printf("[Broadcast] Cliente %d lento en canal %s, descartando frame", id, channel)
					lastLogTime[channel] = currentTime
				}
			}
		}
	} else {
		if lastLog, exists := lastLogTime[channel]; !exists || currentTime.Sub(lastLog) > 5*time.Second {
			log.Printf("[Broadcast] Canal %s no tiene clientes registrados", channel)
			lastLogTime[channel] = currentTime
		}
	}
	channelClientsMtx.Unlock()
}

type Client struct {
	ch   chan []byte
	done chan struct{}
}
