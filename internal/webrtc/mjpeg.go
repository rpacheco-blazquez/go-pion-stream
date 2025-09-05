package webrtc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/http"
	"sync"
)

var (
	clients           = map[int]*mjpegClient{}
	clientsMtx        sync.Mutex
	channelClients    = make(map[string]map[int]*Client)
	channelClientsMtx sync.Mutex
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
	channelClientsMtx.Unlock()

	defer func() {
		channelClientsMtx.Lock()
		delete(channelClients[code], clientID)
		if len(channelClients[code]) == 0 {
			delete(channelClients, code)
		}
		channelClientsMtx.Unlock()
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
	log.Printf("[Broadcast] Enviando frame al canal %s", channel)
	channelClientsMtx.Lock()
	defer channelClientsMtx.Unlock()
	if clients, ok := channelClients[channel]; ok {
		log.Printf("[Broadcast] Canal %s tiene %d clientes registrados", channel, len(clients))
		for id, c := range clients {
			select {
			case c.ch <- frame:
				log.Printf("[Broadcast] Frame enviado al cliente %d en canal %s", id, channel)
			default:
				log.Printf("[Broadcast] Cliente %d lento en canal %s, descartando frame", id, channel)
			}
		}
	} else {
		log.Printf("[Broadcast] Canal %s no tiene clientes registrados", channel)
	}
}

type Client struct {
	ch   chan []byte
	done chan struct{}
}
