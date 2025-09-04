package webrtc

import (
	"bytes"
	"io"
	"log"
	"os/exec"
)

// RunFFmpegToMJPEG lanza ffmpeg para leer rtp-forwarder.sdp y emite una corriente
// de JPEGs por stdout (image2pipe / mjpeg). Esta función parsea stdout por SOI/EOI
// y llama a broadcastFrame([]byte) para enviar cada JPEG a los clientes.
// RunFFmpegToMJPEG arranca ffmpeg (rtp-forwarder.sdp -> mjpeg pipe) y broadcastea los JPEGs
func RunFFmpegToMJPEG() error {
	log.Println("[FFmpeg] Lanzando ffmpeg en modo image2pipe -> MJPEG...")

	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "debug",
		"-nostdin",
		"-protocol_whitelist", "file,udp,rtp",
		"-re", // leer a ritmo "real" (útil para RTP)
		"-i", "rtp-forwarder.sdp",
		"-flags", "low_delay",
		"-avioflags", "direct",
		"-analyzeduration", "0",
		"-probesize", "32k",
		"-use_wallclock_as_timestamps", "1",
		"-vf", "fps=10,scale=960:540",
		"-pix_fmt", "yuvj420p",
		"-flush_packets", "1",
		"-f", "mjpeg",
		"-q:v", "5",
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

	// Leer stderr (logs de ffmpeg)
	go func() {
		defer stderr.Close()
		buf := make([]byte, 4096)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				// Trim final newline para logs más limpios
				log.Printf("[FFmpeg-STDERR] %s", bytes.TrimRight(buf[:n], "\r\n"))
			}
			if rerr != nil {
				if rerr != io.EOF {
					log.Printf("[FFmpeg-STDERR] Lectura stderr terminó con error: %v", rerr)
				}
				break
			}
		}
	}()

	// Leer stdout y extraer JPEGs completos
	go func() {
		defer stdout.Close()
		// buffer pequeño/moderado: procesamos tan pronto como podamos
		buf := make([]byte, 0, 512*1024) // 512KB inicial
		jpegSOI := []byte{0xFF, 0xD8}
		jpegEOI := []byte{0xFF, 0xD9}
		tmp := make([]byte, 32*1024) // leer en chunks de 32KB

		for {
			n, rerr := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)

				// Extraer todos los frames JPEG que se encuentren en el buffer
				for {
					start := bytes.Index(buf, jpegSOI)
					if start < 0 {
						// no hay inicio aún: si buffer es enorme, recortar para evitar OOM
						if len(buf) > 2*1024*1024 { // 2MB safety cap
							log.Println("[FFmpeg] Buffer creció demasiado sin SOI, truncando a 0")
							buf = buf[:0]
						}
						break
					}
					endRel := bytes.Index(buf[start:], jpegEOI)
					if endRel < 0 {
						// tenemos SOI pero no EOI todavía => esperar más bytes
						// si buf se hace muy grande, truncamos al SOI encontrado (conservar SOI)
						if len(buf) > 8*1024*1024 {
							log.Println("[FFmpeg] Buffer creció >8MB sin EOI, preservando desde SOI")
							newBuf := make([]byte, 0, 512*1024)
							newBuf = append(newBuf, buf[start:]...)
							buf = newBuf
						}
						break
					}
					end := start + endRel + 2 // posicion EOI absoluta

					// Copiar frame
					frame := make([]byte, end-start)
					copy(frame, buf[start:end])

					// Broadcast frame (no bloqueante)
					broadcastFrame(frame)

					// recortar buffer
					buf = buf[end:]
				}
			}

			if rerr != nil {
				if rerr != io.EOF {
					log.Printf("[FFmpeg] Error leyendo stdout: %v", rerr)
				}
				break
			}
		}
		log.Println("[FFmpeg] Finalizó lectura de stdout (ffmpeg terminado).")
	}()

	// Esperar a que ffmpeg termine y devolver error si hubo
	if err := cmd.Wait(); err != nil {
		log.Printf("[FFmpeg] ffmpeg finalizó con error: %v", err)
		return err
	}
	log.Println("[FFmpeg] ffmpeg finalizó correctamente.")
	return nil
}
