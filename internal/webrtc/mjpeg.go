package webrtc

import (
	"log"
	"net/http"

	"github.com/rpacheco-blazquez/go-pion-stream/pkg/relay"
)

func watchHandler(w http.ResponseWriter, r *http.Request, code string, clientID int) {
	log.Printf("[WatchHandler] Viewer intentando conectarse al canal %s", code)
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	var client *relay.Client

	if clientID == 0 {
		// Caso 1: Llamada sin clientID, registrar cliente automáticamente
		connectionManager.CreateChannel(code)
		client = connectionManager.AddClient(code, 0) // Generar clientID automáticamente
		if client == nil {
			log.Printf("[WatchHandler] Error al añadir cliente al canal %s", code)
			http.Error(w, "Error al añadir cliente al canal", http.StatusInternalServerError)
			return
		}
		clientID = client.ID
	} else {
		// Caso 2 y 3: Llamada con clientID
		if channel, exists := connectionManager.ValidateChannel(code); exists {
			var err error
			client, err = channel.GetClient(clientID)
			if err != nil {
				// Caso 2: Añadir cliente si no existe
				client = connectionManager.AddClient(code, clientID)
				if client == nil {
					log.Printf("[WatchHandler] Error al añadir cliente al canal %s", code)
					http.Error(w, "Error al añadir cliente al canal", http.StatusInternalServerError)
					return
				}
			}
		} else {
			http.Error(w, "Canal no encontrado", http.StatusNotFound)
			return
		}
	}

	defer func() {
		connectionManager.RemoveClient(code, clientID)
		log.Printf("[WatchHandler] Cliente %d eliminado del canal %s", clientID, code)
	}()

	log.Printf("[WatchHandler] Viewer conectado exitosamente al canal %s con clientID %d", code, clientID)
	log.Printf("[WatchHandler] Enviando frames al viewer en canal %s", code)
	for {
		select {
		case frame := <-client.Chan:
			// log.Printf("[watchHandler] Client %d is not receiving data. Current stream data: %v", client.ID, frame)
			_, _ = w.Write([]byte("--frame\r\n"))
			_, _ = w.Write([]byte("Content-Type: image/jpeg\r\n\r\n"))
			_, _ = w.Write(frame)
			_, _ = w.Write([]byte("\r\n"))
			flusher.Flush()
		case <-client.Done:
			return
		}
	}
}
