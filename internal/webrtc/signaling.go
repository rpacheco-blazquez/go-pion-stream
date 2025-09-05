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

	// Verificar si hay viewers activos en el canal antes de procesar la oferta SDP
	channelClientsMtx.Lock()
	clients, exists := channelClients[code]
	channelClientsMtx.Unlock()
	if !exists || len(clients) == 0 {
		log.Printf("[Signaling] No hay viewers activos en el canal: %s", code)
		http.Error(w, "No hay viewers activos en el canal", http.StatusBadRequest)
		return
	}

	log.Printf("[Signaling] Viewers activos encontrados en el canal: %s", code)

	log.Println("[Signaling] Oferta SDP recibida. Creando sesión WebRTC...")
	// Crear PeerConnection y procesar la oferta
	peerConnection, answer, err := CreateWebRTCSession(offerMsg, code)
	if err != nil {
		log.Println("[Signaling] Error creando sesión WebRTC:", err)
		http.Error(w, "Error interno WebRTC", http.StatusInternalServerError)
		return
	}


	// Asociar el PeerConnection al canal
	CreateChannel(code, peerConnection)


	log.Println("[Signaling] Respondiendo con answer SDP.")
	// Responder con el answer SDP
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SDPMessage{
		Type: answer.Type.String(),
		SDP:  answer.SDP,
	})
}
