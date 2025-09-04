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
	conn, ok := udpConns[track.Kind().String()]
	if !ok {
		return
	}
	buf := make([]byte, 1500)
	rtpPacket := &rtp.Packet{}
	// Solo lanzamos el pipeline MJPEG, ya no grabamos WebM
	// Lanzar ffmpeg MJPEG solo una vez al recibir el primer track de vídeo
	if track.Kind().String() == "video" {
		ffmpegMJPEGOnce.Do(func() {
			if FFMPEGWEBM {
				go func() {
					for {
						timestamp := fmt.Sprintf("%d.webm", getTimestamp())
						log.Printf("[FFmpeg] Lanzando grabación WebM: %s", timestamp)
						err := RunFFmpegToMJPEG()
						if err != nil {
							log.Printf("[FFmpeg] Error WebM: %v", err)
						}
					}
				}()
			} else if isJPEGsaved {
				go func() {
					log.Println("[MJPEG] Guardando frame JPEG manual (blue/red) en bucle...")
					for {
						if err := RunFFmpegToMJPEG(); err != nil {
							log.Printf("[MJPEG] Error guardando frame manual: %v", err)
						}
						time.Sleep(1 * time.Second)
					}
				}()
			} else {
				go func() {
					log.Println("[FFmpeg] Lanzando MJPEG en directo desde HandleTrack...")
					if err := RunFFmpegToMJPEG(); err != nil {
						log.Printf("[FFmpeg] Error MJPEG: %v", err)
					}
				}()
			}
		})
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
		if n, err := rtpPacket.MarshalTo(buf); err != nil {
			log.Printf("[OnTrack] Error marshal RTP: %v", err)
			return
		} else {
			if _, writeErr := conn.conn.Write(buf[:n]); writeErr != nil {
				var opError *net.OpError
				if errors.As(writeErr, &opError) && opError.Err.Error() == "write: connection refused" {
					continue
				}
				log.Printf("[OnTrack] Error escribiendo UDP: %v", writeErr)
				return
			} else {
				if track.Kind().String() == "video" {
					log.Printf("[OnTrack] Paquete RTP de video enviado a UDP puerto %d, tamaño %d bytes", conn.port, n)
				}
			}
		}
	}
}

// streamHandler sirve el archivo static/stream.html
func streamHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/stream.html")
}
