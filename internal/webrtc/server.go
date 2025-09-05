package webrtc

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// Estructura para gestionar los códigos de canal
type ChannelManager struct {
	mu       sync.Mutex
	channels map[string]bool
}

var channelManager = ChannelManager{
	channels: make(map[string]bool),
}

func (cm *ChannelManager) RegisterChannel(code string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.channels[code] = true
	log.Printf("[ChannelManager] Canal registrado: %s", code)
}

func (cm *ChannelManager) ValidateChannel(code string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	_, exists := cm.channels[code]
	return exists
}

// watchUIHandler sirve el visor MJPEG HTML
func watchUIHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		code = generateRandomCode()
	}
	channelManager.RegisterChannel(code)

	http.ServeFile(w, r, "./static/watch.html")
}

// Función para generar códigos aleatorios
func generateRandomCode() string {
	return fmt.Sprintf("%06X", rand.Intn(0xFFFFFF))
}

// StartWebRTCServer inicia el servidor HTTP y configura las rutas principales
func StartWebRTCServer(port int) {
	log.Println("[Server] Iniciando configuración de rutas HTTP...")
	// Actualizar las rutas para manejar códigos de canal
	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Código de canal requerido", http.StatusBadRequest)
			return
		}
		handleWebRTCStream(w, r, code)
	})

	http.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Código de canal requerido", http.StatusBadRequest)
			return
		}
		watchHandler(w, r, code)
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
func CreateWebRTCSession(offer SDPMessage, code string) (*webrtc.PeerConnection, *webrtc.SessionDescription, error) {
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
		return nil, nil, err
	}

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
		go HandleTrack(track, udpConns, code)

		// Sólo pedir PLIs para video
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			go func(pc *webrtc.PeerConnection, ssrc uint32) {
				sendInitialPLIs(pc, ssrc, 5, 300*time.Millisecond)
			}(peerConnection, uint32(track.SSRC()))
		}
	})

	// 7. Callbacks de estado
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("[ICE] State: %s", connectionState.String())
	})
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[PeerConnection] State: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			log.Println("[PeerConnection] Cerrando conexiones UDP")
			for _, conn := range udpConns {
				conn.conn.Close()
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
	if !channelManager.ValidateChannel(code) {
		log.Printf("[Signaling] Código de canal inválido: %s", code)
		http.Error(w, "Código de canal inválido", http.StatusBadRequest)
		return
	}

	// Verificar si hay viewers activos en el canal
	channelClientsMtx.Lock()
	clients, exists := channelClients[code]
	channelClientsMtx.Unlock()
	if !exists || len(clients) == 0 {
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

	// Verificar si el canal ya existe
	if channelManager.ValidateChannel(code) {
		log.Printf("[RegisterHandler] Canal ya existe: %s", code)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Registrar el canal si no existe
	channelManager.RegisterChannel(code)
	w.WriteHeader(http.StatusOK)
	log.Printf("[RegisterHandler] Código registrado: %s", code)
}
