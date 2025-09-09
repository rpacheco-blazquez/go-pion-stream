package webrtc

import (
	"net/http"

	"github.com/rpacheco-blazquez/go-pion-stream/pkg/relay"
)

func watchHandler(w http.ResponseWriter, r *http.Request, code string, clientID int) {
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
		connectionManager.CreateChannel(code)
		client = connectionManager.AddClient(code, 0)
		if client == nil {
			http.Error(w, "Error al añadir cliente al canal", http.StatusInternalServerError)
			return
		}
		clientID = client.ID
	} else {
		if channel, exists := connectionManager.ValidateChannel(code); exists {
			var err error
			client, err = channel.GetClient(clientID)
			if err != nil {
				client = connectionManager.AddClient(code, clientID)
				if client == nil {
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
	}()

	for {
		select {
		case frame := <-client.Chan:
			_, _ = w.Write([]byte("--frame\r\n"))
			_, _ = w.Write([]byte("Content-Type: image/jpeg\r\n\r\n"))
			_, _ = w.Write(frame)
			_, _ = w.Write([]byte("\r\n"))
			flusher.Flush()
		case <-client.Done:
			return
		case <-r.Context().Done():
			return
		}
	}
}
