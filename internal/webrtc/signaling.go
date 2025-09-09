package webrtc

import (
	"encoding/json"
	"net/http"
)

// SDPMessage representa la estructura JSON intercambiada con el frontend
type SDPMessage struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// handleWebRTCStream maneja la señalización WebRTC vía HTTP POST
func handleWebRTCStream(w http.ResponseWriter, r *http.Request, code string) {
	var offerMsg SDPMessage
	if err := json.NewDecoder(r.Body).Decode(&offerMsg); err != nil {
		http.Error(w, "SDP inválido", http.StatusBadRequest)
		return
	}
	channel, exists := connectionManager.ValidateChannel(code)
	if !exists {
		http.Error(w, "Canal no encontrado", http.StatusBadRequest)
		return
	}
	clients := connectionManager.ListClients(code)
	if len(clients) == 0 {
		connectionManager.RemoveChannel(code)
		http.Error(w, "Ya no hay viewers activos en el canal. El canal ha sido eliminado.", http.StatusBadRequest)
		return
	}
	streamID := len(connectionManager.ListAllStreams()) + 1
	if _, err := channel.AttachStream(streamID); err != nil {
		http.Error(w, "Error interno creando el stream", http.StatusInternalServerError)
		return
	}
	peerConnection, answer, err := CreateWebRTCSession(offerMsg, code, streamID)
	if err != nil {
		http.Error(w, "Error interno WebRTC", http.StatusInternalServerError)
		return
	}
	if err := channel.AssociateStreamPeerConnection(streamID, peerConnection); err != nil {
		http.Error(w, "Error interno asociando PeerConnection", http.StatusInternalServerError)
		return
	}
	err = channel.StartStream(streamID)
	if err != nil {
		http.Error(w, "Error interno iniciando el stream", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SDPMessage{
		Type: answer.Type.String(),
		SDP:  answer.SDP,
	})
}
