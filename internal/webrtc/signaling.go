package webrtc

import (
	"encoding/json"
	"log"
	"net/http"
)

// SDPMessage representa la estructura JSON intercambiada con el frontend
type SDPMessage struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// handleWebRTCStream maneja la señalización WebRTC vía HTTP POST
func handleWebRTCStream(w http.ResponseWriter, r *http.Request, code string) {
	log.Printf("[Signaling] Petición recibida en /stream para el canal %s", code)

	var offerMsg SDPMessage
	if err := json.NewDecoder(r.Body).Decode(&offerMsg); err != nil {
		log.Println("[Signaling] Error decodificando SDP:", err)
		http.Error(w, "SDP inválido", http.StatusBadRequest)
		return
	}

	log.Printf("[Signaling] Código de canal recibido: %s", code)

	// Validar el canal y verificar si hay viewers activos
	channel, exists := connectionManager.ValidateChannel(code)
	log.Printf("[Signaling] Validación de canal para %s: existe=%v", code, exists)
	if !exists {
		log.Printf("[Signaling] Canal no existe: %s", code)
		http.Error(w, "Canal no encontrado", http.StatusBadRequest)
		return
	}

	clients := connectionManager.ListClients(code)
	log.Printf("[Signaling] Número de viewers obtenidos para el canal %s: %d", code, len(clients))

	if len(clients) == 0 {
		log.Printf("[Signaling] Ya no hay viewers activos en el canal: %s. El canal será eliminado.", code)
		connectionManager.RemoveChannel(code) // Remove the channel if no viewers are active
		http.Error(w, "Ya no hay viewers activos en el canal. El canal ha sido eliminado.", http.StatusBadRequest)
		return
	}

	log.Printf("[Signaling] Viewers activos encontrados en el canal: %s", code)

	log.Println("[Signaling] Oferta SDP recibida. Creando sesión WebRTC...")
	// Obtener un streamID único
	streamID := len(connectionManager.ListAllStreams()) + 1

	// Crear el stream con el streamID generado
	if _, err := channel.AttachStream(streamID); err != nil {
		log.Println("[Signaling] Error creando el stream:", err)
		http.Error(w, "Error interno creando el stream", http.StatusInternalServerError)
		return
	}

	// Crear PeerConnection y procesar la oferta
	peerConnection, answer, err := CreateWebRTCSession(offerMsg, code, streamID)
	if err != nil {
		log.Println("[Signaling] Error creando sesión WebRTC:", err)
		http.Error(w, "Error interno WebRTC", http.StatusInternalServerError)
		return
	}

	// Asociar el PeerConnection al canal
	if err := channel.AssociateStreamPeerConnection(streamID, peerConnection); err != nil {
		log.Println("[Signaling] Error asociando PeerConnection al canal:", err)
		http.Error(w, "Error interno asociando PeerConnection", http.StatusInternalServerError)
		return
	}

	// Iniciar el stream a nivel de canal
	log.Printf("[Signaling] Intentando iniciar el stream con streamID: %d", streamID)
	err = channel.StartStream(streamID)
	if err != nil {
		log.Printf("[Signaling] Error iniciando el stream: %v", err)
		http.Error(w, "Error interno iniciando el stream", http.StatusInternalServerError)
		return
	}

	log.Println("[Signaling] Respondiendo con answer SDP.")
	// Responder con el answer SDP
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SDPMessage{
		Type: answer.Type.String(),
		SDP:  answer.SDP,
	})
}
