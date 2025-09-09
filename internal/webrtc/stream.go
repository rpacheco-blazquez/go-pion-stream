package webrtc

import (
	"context"
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
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		channel, exists := connectionManager.ValidateChannel(code)
		if exists && !channel.IsFFmpegMJPEGActive() {
			channel.SetFFmpegMJPEGActive(true)
			ctx, cancel := context.WithCancel(context.Background())
			channel.SetFFmpegMJPEGCancel(cancel)
			go func() {
				err := RunFFmpegToMJPEG(ctx, func(frame []byte) {
					activeID := channel.GetActiveStreamID()
					if activeID != nil {
						connectionManager.BroadcastToStream(code, *activeID, frame)
					} else {
						return
					}
				})
				if err != nil {
					// Only log critical error
					log.Printf("[HandleTrack] Error al ejecutar RunFFmpegToMJPEG: %v", err)
				}
				channel.SetFFmpegMJPEGActive(false)
				channel.SetFFmpegMJPEGCancel(nil)
			}()
		}
	}
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
