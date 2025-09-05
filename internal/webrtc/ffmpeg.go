package webrtc

import (
	"bytes"
	"io"
	"log"
	"os/exec"
)

// RunFFmpegToMJPEG lanza ffmpeg para leer rtp-forwarder.sdp y emite una corriente
// de JPEGs por stdout (image2pipe / mjpeg). Cada frame JPEG completo se pasa al callback onFrame.
// Esto permite que MJPEG reciba frames incluso si no hay clientes aún.
func RunFFmpegToMJPEG(onFrame func([]byte)) error {
	log.Println("[FFmpeg] Lanzando ffmpeg en modo image2pipe -> MJPEG...")

	// Flag para activar/desactivar logs de FFmpeg
	logFFmpeg := true

	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "debug", // comentado para no ralentizar por logging
		"-nostdin",
		"-protocol_whitelist", "file,udp,rtp",
		// "-re", // comentado para evitar buffering y delay
		"-i", "rtp-forwarder.sdp",
		"-an",                // quitar audio, no nos interesa el audio
		"-vf", "scale=-1:-1", // mantiene resolución original
		"-c:v", "mjpeg", // codec MJPEG
		"-q:v", "8", // calidad moderada, balance CPU/latencia
		// "-r", "1",           // comentado, no forzar framerate bajo
		"-f", "mjpeg",
		"pipe:1", // salida a stdout
	)

	cmd.Dir = "."

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[FFmpeg] Error creando StdoutPipe: %v", err)
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[FFmpeg] Error creando StderrPipe: %v", err)
		return err
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[FFmpeg] Error lanzando ffmpeg: %v", err)
		return err
	}

	// Leer stderr (logs de ffmpeg) solo si logFFmpeg está activado
	go func() {
		defer stderr.Close()
		buf := make([]byte, 4096)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 && logFFmpeg {
				log.Printf("[FFmpeg-STDERR] %s", bytes.TrimRight(buf[:n], "\r\n"))
			}
			if rerr != nil {
				if rerr != io.EOF && logFFmpeg {
					log.Printf("[FFmpeg-STDERR] Lectura stderr terminó con error: %v", rerr)
				}
				break
			}
		}
	}()

	// Leer stdout y extraer JPEGs completos
	go func() {
		defer stdout.Close()
		buf := make([]byte, 0, 512*1024) // 512KB inicial
		jpegSOI := []byte{0xFF, 0xD8}
		jpegEOI := []byte{0xFF, 0xD9}
		tmp := make([]byte, 32*1024)

		for {
			n, rerr := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)

				// Extraer todos los frames JPEG completos del buffer
				for {
					start := bytes.Index(buf, jpegSOI)
					if start < 0 {
						if len(buf) > 2*1024*1024 {
							buf = buf[:0] // safety cap
						}
						break
					}
					endRel := bytes.Index(buf[start:], jpegEOI)
					if endRel < 0 {
						if len(buf) > 8*1024*1024 {
							newBuf := make([]byte, 0, 512*1024)
							newBuf = append(newBuf, buf[start:]...)
							buf = newBuf
						}
						break
					}
					end := start + endRel + 2
					frame := make([]byte, end-start)
					copy(frame, buf[start:end])

					// Llamar callback (broadcast o lo que quieras)
					if onFrame != nil {
						onFrame(frame)
					}
					log.Println("[FFmpeg] Frame generado y enviado al handler broadcastFrameToChannel.")

					buf = buf[end:]
				}
			}

			if rerr != nil {
				if rerr != io.EOF && logFFmpeg {
					log.Printf("[FFmpeg] Error leyendo stdout: %v", rerr)
				}
				break
			}
		}
		if logFFmpeg {
			log.Println("[FFmpeg] Finalizó lectura de stdout (ffmpeg terminado).")
		}
	}()

	if err := cmd.Wait(); err != nil {
		if logFFmpeg {
			log.Printf("[FFmpeg] ffmpeg finalizó con error: %v", err)
		}
		return err
	}
	if logFFmpeg {
		log.Println("[FFmpeg] ffmpeg finalizó correctamente.")
	}
	return nil
}
