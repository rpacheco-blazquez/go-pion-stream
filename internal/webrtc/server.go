package webrtc

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"

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
	log.Printf("[watchUIHandler] Sirviendo watch.html")

	http.ServeFile(w, r, "./static/watch.html")
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
	log.Println("[Server] Iniciando configuración de rutas HTTP...")
	// Actualizar las rutas para manejar códigos de canal
	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		log.Println("[/stream] Solicitud recibida para canal:", code)

		_, valid := connectionManager.ValidateChannel(code)
		if !valid {
			log.Println("[/stream] Canal no válido:", code)
			http.Error(w, "Canal no válido", http.StatusBadRequest)
			return
		}
		log.Println("[/stream] Canal validado:", code)

		clientIDParam := r.URL.Query().Get("clientID")
		if clientIDParam != "" {
			clientID, err := strconv.Atoi(clientIDParam)
			if err != nil {
				log.Println("[/stream] clientID inválido:", clientIDParam)
				http.Error(w, "clientID inválido", http.StatusBadRequest)
				return
			}
			log.Println("[/stream] clientID recibido:", clientID)
		}

		log.Println("[/stream] Llamando a CreateWebRTCSession para canal:", code)
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
	// http.HandleFunc("/", rootHandler)

	// Nuevo handler para registrar códigos
	http.HandleFunc("/register", registerHandler)

	log.Println("[Server] Escuchando en puerto", port, "(endpoints /watch, /stream, /streamui, /watchui)")
	addr := ":" + strconv.Itoa(port)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// CreateWebRTCSession inicializa una sesión WebRTC, procesa la oferta y devuelve el answer
func CreateWebRTCSession(offer SDPMessage, code string, streamID int) (*webrtc.PeerConnection, *webrtc.SessionDescription, error) {
	log.Println("[WebRTC] Creando nueva sesión WebRTC...")
	log.Println("[CreateWebRTCSession] Iniciando sesión WebRTC para canal:", code)

	// 1. Inicializar MediaEngine y codecs
	mediaEngine := &webrtc.MediaEngine{}
	log.Println("[WebRTC] Registrando codecs...")
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Println("[CreateWebRTCSession] Error registrando codec:", err)
		return nil, nil, err
	}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
	}, webrtc.RTPCodecTypeAudio); err != nil {
		log.Println("[CreateWebRTCSession] Error registrando codec:", err)
		return nil, nil, err
	}
	log.Println("[CreateWebRTCSession] Codec registrado correctamente")

	// 2. InterceptorRegistry y PLI
	interceptorRegistry := &interceptor.Registry{}
	log.Println("[WebRTC] Configurando interceptores...")
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		return nil, nil, err
	}
	interceptorRegistry.Add(intervalPliFactory)
	if err = webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		return nil, nil, err
	}

	// 3. Crear API y PeerConnection
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(interceptorRegistry))
	log.Println("[WebRTC] Creando PeerConnection...")
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		log.Println("[CreateWebRTCSession] Error creando PeerConnection:", err)
		return nil, nil, err
	}
	log.Println("[CreateWebRTCSession] PeerConnection creado correctamente")

	// 4. Transceivers
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		return nil, nil, err
	}
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}

	// 5. Preparar UDP para reenviar RTP
	udpConns := map[string]*udpConn{
		"audio": {port: 4000, payloadType: 111},
		"video": {port: 4002, payloadType: 96},
	}
	laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:")
	if err != nil {
		return nil, nil, err
	}
	for _, conn := range udpConns {
		raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", conn.port))
		if err != nil {
			return nil, nil, err
		}
		conn.conn, err = net.DialUDP("udp", laddr, raddr)
		if err != nil {
			return nil, nil, err
		}
	}

	// 6. OnTrack: reenviar RTP a UDP
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[WebRTC] Recibido nuevo track: kind=%s, ssrc=%d, id=%s\n", track.Kind().String(), track.SSRC(), track.ID())

		// Lanzamos un goroutine para reenviar los RTP
		go HandleTrack(track, udpConns, code, streamID)

		// Sólo pedir PLIs para video
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			go func(pc *webrtc.PeerConnection, ssrc uint32) {
				sendInitialPLIs(pc, ssrc, 5, 300*time.Millisecond)
			}(peerConnection, uint32(track.SSRC()))
		}
	})

	// 7. Callbacks de estado
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("[ICE] Estado de conexión ICE: %s", connectionState.String())
	})
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[PeerConnection] Estado de conexión: %s", state.String())
		log.Printf("[OnConnectionStateChange] Estado de conexión: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			log.Println("[PeerConnection] Cerrando conexiones UDP")
			for _, conn := range udpConns {
				conn.conn.Close()
			}

			// Obtener el canal y detener el stream asociado
			if channel, exists := connectionManager.ValidateChannel(code); exists {
				if err := channel.StopStream(streamID); err != nil {
					log.Printf("[PeerConnection] Error deteniendo el stream: %v", err)
				}
			}
		}
	})

	// 8. Procesar la oferta SDP
	remoteOffer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}
	if err = peerConnection.SetRemoteDescription(remoteOffer); err != nil {
		return nil, nil, err
	}

	// 9. Crear answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return nil, nil, err
	}
	<-gatherComplete

	// 10. Devolver PeerConnection y answer SDP
	log.Printf("[CreateWebRTCSession] Configuración completada para streamID: %d en canal: %s", streamID, code)
	return peerConnection, peerConnection.LocalDescription(), nil
}

// CreateWebRTCSessionWithPeerConnection inicializa una sesión WebRTC y devuelve el PeerConnection y el answer SDP
func CreateWebRTCSessionWithPeerConnection(offer SDPMessage) (*webrtc.PeerConnection, *webrtc.SessionDescription, error) {
	log.Println("[WebRTC] Creando nueva sesión WebRTC...")
	// 1. Inicializar MediaEngine y codecs
	mediaEngine := &webrtc.MediaEngine{}
	log.Println("[WebRTC] Registrando codecs...")
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

	// 2. Crear API y PeerConnection
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	log.Println("[WebRTC] Creando PeerConnection...")
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, nil, err
	}

	// 3. Procesar la oferta SDP
	remoteOffer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}
	if err = peerConnection.SetRemoteDescription(remoteOffer); err != nil {
		return nil, nil, err
	}

	// 4. Crear answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return nil, nil, err
	}
	<-gatherComplete

	// 5. Devolver PeerConnection y answer SDP
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
