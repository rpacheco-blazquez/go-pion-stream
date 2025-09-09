package webrtc

import (
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"

	cfg "github.com/rpacheco-blazquez/go-pion-stream/internal/config"
	"github.com/rpacheco-blazquez/go-pion-stream/pkg/relay"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// Global instance of ConnectionManager
var connectionManager = relay.NewConnectionManager()

// watchUIHandler sirve el visor MJPEG HTML
func watchUIHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/watch.html")
}

// logUIHandler sirve el visor de logs HTML
func logUIHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/log.html")
}

// Función para generar códigos aleatorios
func generateRandomCode() string {
	return fmt.Sprintf("%06X", rand.Intn(0xFFFFFF))
}

// Function to generate client IDs based on the length of all clients
func generateClientID() int {
	return len(connectionManager.ListAllClients()) + 1
}

// StartWebRTCServer inicia el servidor HTTP y configura las rutas principales
func StartWebRTCServer(port int) {
	// Actualizar las rutas para manejar códigos de canal
	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		_, valid := connectionManager.ValidateChannel(code)
		if !valid {
			http.Error(w, "Canal no válido", http.StatusBadRequest)
			return
		}
		clientIDParam := r.URL.Query().Get("clientID")
		if clientIDParam != "" {
			clientID, err := strconv.Atoi(clientIDParam)
			if err != nil {
				http.Error(w, "clientID inválido", http.StatusBadRequest)
				return
			}
			_ = clientID // Only parse, do not log
		}
		handleWebRTCStream(w, r, code)
	})

	http.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Código de canal requerido", http.StatusBadRequest)
			return
		}

		clientIDParam := r.URL.Query().Get("clientID")
		var clientID int
		var err error
		if clientIDParam != "" {
			clientID, err = strconv.Atoi(clientIDParam)
			if err != nil {
				http.Error(w, "clientID inválido", http.StatusBadRequest)
				return
			}
		}

		// Pasar code y clientID al watchHandler
		watchHandler(w, r, code, clientID)
	})

	http.HandleFunc("/streamui", streamHandler) // servir HTML
	http.HandleFunc("/watchui", watchUIHandler) // servir visor MJPEG
	http.HandleFunc("/log", logUIHandler)       // servir visor de logs

	// Nuevo handler para registrar códigos
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/webrtc_server.log", logFileHandler) // servir log por HTTP

	// Endpoint principal explicativo con enlaces (usa template)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("static/index.tmpl")
		if err != nil {
			http.Error(w, "Error cargando plantilla", http.StatusInternalServerError)
			log.Printf("[TEMPLATE] Error al cargar index.tmpl: %v", err)
			return
		}
		data := struct {
			NgrokURL string
		}{
			NgrokURL: cfg.GetNgrokPublicURL(),
		}
		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, "Error procesando plantilla", http.StatusInternalServerError)
			log.Printf("[TEMPLATE] Error al ejecutar index.tmpl: %v", err)
		}
	})

	addr := ":" + strconv.Itoa(port)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// logFileHandler sirve el archivo de log bruto
func logFileHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "webrtc_server.log")
}

// CreateWebRTCSession inicializa una sesión WebRTC, procesa la oferta y devuelve el answer
func CreateWebRTCSession(offer SDPMessage, code string, streamID int) (*webrtc.PeerConnection, *webrtc.SessionDescription, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, nil, err
	}
	interceptorRegistry := &interceptor.Registry{}
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		return nil, nil, err
	}
	interceptorRegistry.Add(intervalPliFactory)
	if err = webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		return nil, nil, err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(interceptorRegistry))
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, nil, err
	}
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		return nil, nil, err
	}
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}
	udpConns := map[string]*udpConn{
		"audio": {port: 4000, payloadType: 111},
		"video": {port: 4002, payloadType: 96},
	}
	configVals := cfg.Load()
	udpBindIP := configVals.UDPBindIP
	laddr, err := net.ResolveUDPAddr("udp", udpBindIP+":")
	if err != nil {
		return nil, nil, err
	}
	for _, conn := range udpConns {
		raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", udpBindIP, conn.port))
		if err != nil {
			return nil, nil, err
		}
		conn.conn, err = net.DialUDP("udp", laddr, raddr)
		if err != nil {
			return nil, nil, err
		}
	}
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go HandleTrack(track, udpConns, code, streamID)
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			go func(pc *webrtc.PeerConnection, ssrc uint32) {
				sendInitialPLIs(pc, ssrc, 5, 300*time.Millisecond)
			}(peerConnection, uint32(track.SSRC()))
		}
	})
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {})
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			for _, conn := range udpConns {
				conn.conn.Close()
			}
			if channel, exists := connectionManager.ValidateChannel(code); exists {
				_ = channel.StopStream(streamID)
			}
		}
	})
	remoteOffer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}
	if err = peerConnection.SetRemoteDescription(remoteOffer); err != nil {
		return nil, nil, err
	}
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return nil, nil, err
	}
	<-gatherComplete
	return peerConnection, peerConnection.LocalDescription(), nil
}

// CreateWebRTCSessionWithPeerConnection inicializa una sesión WebRTC y devuelve el PeerConnection y el answer SDP
func CreateWebRTCSessionWithPeerConnection(offer SDPMessage) (*webrtc.PeerConnection, *webrtc.SessionDescription, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, nil, err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, nil, err
	}
	remoteOffer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}
	if err = peerConnection.SetRemoteDescription(remoteOffer); err != nil {
		return nil, nil, err
	}
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return nil, nil, err
	}
	<-gatherComplete
	return peerConnection, peerConnection.LocalDescription(), nil
}

// sendInitialPLIs envía hasta maxRetries PLIs con un backoff entre intentos.
// - pc: PeerConnection para usar WriteRTCP
// - mediaSSRC: SSRC del track de video
// - maxRetries: cuántos intentos como máximo (p. ej. 5)
// - initialInterval: intervalo inicial entre PLIs (p. ej. 300ms). Hago backoff multiplicando por 2.
func sendInitialPLIs(pc *webrtc.PeerConnection, mediaSSRC uint32, maxRetries int, initialInterval time.Duration) {
	interval := initialInterval
	for i := 0; i < maxRetries; i++ {
		pli := &rtcp.PictureLossIndication{MediaSSRC: mediaSSRC}
		if err := pc.WriteRTCP([]rtcp.Packet{pli}); err != nil {
			log.Printf("[RTCP] Error enviando PLI (intento %d/%d) ssrc=%d: %v", i+1, maxRetries, mediaSSRC, err)
		} else {
			log.Printf("[RTCP] PLI enviado (intento %d/%d) ssrc=%d", i+1, maxRetries, mediaSSRC)
		}

		// Esperar antes del siguiente intento (backoff exponencial)
		time.Sleep(interval)
		interval *= 2
	}
	log.Printf("[RTCP] Finalizados intentos de PLI para ssrc=%d (max %d intentos)", mediaSSRC, maxRetries)
}

// Validar el código en el handler de /stream
func validateStreamHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	channel, exists := connectionManager.ValidateChannel(code)
	if !exists {
		log.Printf("[Signaling] Código de canal inválido: %s", code)
		http.Error(w, "Código de canal inválido", http.StatusBadRequest)
		return
	}

	// Verificar si hay viewers activos en el canal
	clients := channel.ListClients()
	if len(clients) == 0 {
		log.Printf("[Signaling] No hay viewers activos en el canal: %s", code)
		http.Error(w, "No hay viewers activos en el canal", http.StatusBadRequest)
		return
	}

	log.Printf("[Signaling] Código de canal válido y viewers activos: %s", code)
}

// Handler para registrar códigos desde /register
func registerHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Código no proporcionado", http.StatusBadRequest)
		return
	}

	// Crear o validar el canal
	connectionManager.CreateChannel(code)

	// Añadir el cliente al canal
	clientID := generateClientID()
	client := connectionManager.AddClient(code, clientID)
	if client == nil {
		log.Printf("[RegisterHandler] Error al añadir cliente al canal %s", code)
		http.Error(w, "Error al añadir cliente al canal", http.StatusInternalServerError)
		return
	}

	log.Printf("[RegisterHandler] Viewer conectado exitosamente al canal %s con clientID %d", code, clientID)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%d", clientID)))
}
