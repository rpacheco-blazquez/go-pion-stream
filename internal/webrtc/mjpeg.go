package webrtc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	clientBufSize = 4 // cuantos frames puede acumular cada cliente
)

var (
	// map de clientes y su mutex
	clients    = map[int]*mjpegClient{}
	clientsMtx sync.Mutex
	nextID     = 1

	// asegura que ffmpeg se arranque solo la primera vez
	ffmpegOnce sync.Once
)

// mjpegClient representa un consumidor conectado a /watch
type mjpegClient struct {
	id   int
	ch   chan []byte
	done chan struct{}
}

func newMJPEGClient() *mjpegClient {
	clientsMtx.Lock()
	id := nextID
	nextID++
	clientsMtx.Unlock()

	return &mjpegClient{
		id:   id,
		ch:   make(chan []byte, clientBufSize),
		done: make(chan struct{}),
	}
}

func addClient(c *mjpegClient) {
	clientsMtx.Lock()
	clients[c.id] = c
	clientsMtx.Unlock()
	// arrancar ffmpeg la primera vez que llegue un cliente
	ffmpegOnce.Do(func() {
		log.Println("[MJPEG] Primer cliente: arrancando RunFFmpegToMJPEG()")
		go func() {
			if err := RunFFmpegToMJPEG(); err != nil {
				log.Printf("[MJPEG] RunFFmpegToMJPEG terminó con error: %v", err)
			}
		}()
	})
	// enviar frame negro inicial para que el cliente tenga algo inmediato
	select {
	case c.ch <- makeBlackJPEG():
	default:
	}
}

func removeClient(c *mjpegClient) {
	clientsMtx.Lock()
	if _, ok := clients[c.id]; ok {
		delete(clients, c.id)
		close(c.done)
		close(c.ch)
	}
	clientsMtx.Unlock()
}

// broadcastFrame envía el frame a todos los clientes (no bloqueante)
func broadcastFrame(frame []byte) {
	clientsMtx.Lock()
	defer clientsMtx.Unlock()
	for id, c := range clients {
		select {
		case c.ch <- frame:
			// enviado
		default:
			// cliente lento -> descartamos el frame para no bloquear
			log.Printf("[MJPEG] cliente %d lento, descartando frame", id)
		}
	}
}

// crea un JPEG negro (usado al conectar)
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

// watchHandler sirve un stream MJPEG multipart a cada cliente conectado
func watchHandler(w http.ResponseWriter, r *http.Request) {
	// Encabezados iniciales para multipart MJPEG
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// asegúrate de que es posible flushear
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// registrar cliente
	client := newMJPEGClient()
	addClient(client)
	defer removeClient(client)

	// Contexto para detectar desconexión del cliente
	notify := r.Context().Done()

	// Loop: cada vez que haya un frame en el channel lo mandamos
	for {
		select {
		case <-notify:
			// cliente desconectado
			return
		case <-client.done:
			return
		case frame, ok := <-client.ch:
			if !ok {
				return
			}
			// escribir parte multipart
			if _, err := w.Write([]byte("--frame\r\n")); err != nil {
				return
			}
			if _, err := w.Write([]byte("Content-Type: image/jpeg\r\n")); err != nil {
				return
			}
			if _, err := w.Write([]byte("Content-Length: " + strconv.Itoa(len(frame)) + "\r\n\r\n")); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-time.After(5 * time.Second):
			// Si no hay frames durante un tiempo razonable, enviar un frame negro para mantener conexión viva
			black := makeBlackJPEG()
			if _, err := w.Write([]byte("--frame\r\n")); err != nil {
				return
			}
			if _, err := w.Write([]byte("Content-Type: image/jpeg\r\n")); err != nil {
				return
			}
			if _, err := w.Write([]byte("Content-Length: " + strconv.Itoa(len(black)) + "\r\n\r\n")); err != nil {
				return
			}
			if _, err := w.Write(black); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
