package webrtc

import (
	"errors"
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

// HandleTrack reempaqueta y reenvía RTP a UDP según el tipo de track
var ffmpegMJPEGOnce sync.Once

func HandleTrack(track *webrtc.TrackRemote, udpConns map[string]*udpConn, code string, streamID int) {
	buf := make([]byte, 1500)
	rtpPacket := &rtp.Packet{}

	// Si es vídeo, arrancamos FFmpeg solo una vez
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		ffmpegMJPEGOnce.Do(func() {
			log.Println("[FFmpeg] Primer track de video, arrancando pipeline MJPEG...")
			go func() {
				// log.Println("[HandleTrack] Llamando a RunFFmpegToMJPEG...")
				err := RunFFmpegToMJPEG(func(frame []byte) {
					// log.Printf("[HandleTrack] Frame recibido en callback para streamID: %d en canal: %s", streamID, code)
					//connectionManager.BroadcastToStream(code, streamID, frame)
					connectionManager.BroadcastToClient(code, frame)
				})
				if err != nil {
					log.Printf("[HandleTrack] Error al ejecutar RunFFmpegToMJPEG: %v", err)
				}
			}()
		})
	}

	// Reenvío de RTP si quieres UDP
	conn, ok := udpConns[track.Kind().String()]
	if !ok {
		return
	}

	logTicker := time.NewTicker(5 * time.Second)
	defer logTicker.Stop()

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

		select {
		case <-logTicker.C:
			log.Printf("[HandleTrack] Frame enviado al stream %d en el canal %s", streamID, code)
			log.Printf("[HandleTrack] Procesando frame para streamID: %d en canal: %s", streamID, code)
		default:
		}
	}
}

// streamHandler sirve el archivo static/stream.html
func streamHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/stream.html")
}
