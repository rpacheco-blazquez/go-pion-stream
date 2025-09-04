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
func handleWebRTCStream(w http.ResponseWriter, r *http.Request) {
	log.Println("[Signaling] Petición recibida en /stream")
	if r.Method != http.MethodPost {
		log.Println("[Signaling] Método no permitido:", r.Method)
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var offerMsg SDPMessage
	if err := json.NewDecoder(r.Body).Decode(&offerMsg); err != nil {
		log.Println("[Signaling] Error decodificando SDP:", err)
		http.Error(w, "SDP inválido", http.StatusBadRequest)
		return
	}

	log.Println("[Signaling] Oferta SDP recibida. Creando sesión WebRTC...")
	// Crear PeerConnection y procesar la oferta
	answer, err := CreateWebRTCSession(offerMsg)
	if err != nil {
		log.Println("[Signaling] Error creando sesión WebRTC:", err)
		http.Error(w, "Error interno WebRTC", http.StatusInternalServerError)
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
