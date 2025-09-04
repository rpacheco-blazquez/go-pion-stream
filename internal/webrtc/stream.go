package webrtc

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"time"
)

// getTimestamp devuelve el timestamp actual en segundos
func getTimestamp() int64 {
	return time.Now().Unix()
}

// udpConn representa una conexión UDP para reenviar RTP
type udpConn struct {
	conn        *net.UDPConn
	port        int
	payloadType uint8
}

// InitUDPConns inicializa los sockets UDP para audio y video
func InitUDPConns() (map[string]*udpConn, error) {
	udpConns := map[string]*udpConn{
		"audio": {port: 4000, payloadType: 111},
		"video": {port: 4002, payloadType: 96},
	}
	laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:")
	if err != nil {
		return nil, err
	}
	for _, conn := range udpConns {
		raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", conn.port))
		if err != nil {
			return nil, err
		}
		conn.conn, err = net.DialUDP("udp", laddr, raddr)
		if err != nil {
			return nil, err
		}
	}
	return udpConns, nil
}

// Variable para alternar entre MJPEG en vivo y grabación WebM
var FFMPEGWEBM = false  // true: graba WebM cada 5s, false: MJPEG en vivo
var isJPEGsaved = false // true: se ha guardado un JPEG manualmente

// HandleTrack reempaqueta y reenvía RTP a UDP según el tipo de track
var ffmpegMJPEGOnce sync.Once

func HandleTrack(track *webrtc.TrackRemote, udpConns map[string]*udpConn) {
	buf := make([]byte, 1500)
	rtpPacket := &rtp.Packet{}

	// Si es vídeo, arrancamos FFmpeg solo una vez
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		ffmpegMJPEGOnce.Do(func() {
			log.Println("[FFmpeg] Primer track de video, arrancando pipeline MJPEG...")
			go func() {
				err := RunFFmpegToMJPEG(func(frame []byte) {
				// cada frame JPEG que genera FFmpeg se envía a todos los clientes MJPEG
					broadcastFrame(frame)
				})
				if err != nil {
					log.Printf("[FFmpeg] Error en pipeline MJPEG: %v", err)
				}
			}()
		})
	}

	// Reenvío de RTP si quieres UDP
	conn, ok := udpConns[track.Kind().String()]
	if !ok {
		return
	}

	for {
		n, _, readErr := track.Read(buf)
		if readErr != nil {
			log.Printf("[OnTrack] Error leyendo track: %v", readErr)
			return
		}

		if err := rtpPacket.Unmarshal(buf[:n]); err != nil {
			log.Printf("[OnTrack] Error unmarshal RTP: %v", err)
			return
		}

		rtpPacket.PayloadType = conn.payloadType
		n, err := rtpPacket.MarshalTo(buf)
		if err != nil {
			log.Printf("[OnTrack] Error marshal RTP: %v", err)
			return
		}

		if _, writeErr := conn.conn.Write(buf[:n]); writeErr != nil {
			var opError *net.OpError
			if errors.As(writeErr, &opError) && opError.Err.Error() == "write: connection refused" {
				continue
			}
			log.Printf("[OnTrack] Error escribiendo UDP: %v", writeErr)
			return
		}
	}
}

// streamHandler sirve el archivo static/stream.html
func streamHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/stream.html")
}
