package webrtc

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
)

// RunFFmpegToMJPEG lanza ffmpeg para leer rtp-forwarder.sdp y emite una corriente
// de JPEGs por stdout (image2pipe / mjpeg). Cada frame JPEG completo se pasa al callback onFrame.
// Esto permite que MJPEG reciba frames incluso si no hay clientes aún.

// ...existing imports...

// RunFFmpegToMJPEG lanza ffmpeg para leer rtp-forwarder.sdp y emite una corriente
// de JPEGs por stdout (image2pipe / mjpeg). Cada frame JPEG completo se pasa al callback onFrame.
// El contexto permite cancelar el proceso ffmpeg y la goroutine.
func RunFFmpegToMJPEG(ctx context.Context, onFrame func([]byte)) error {
	// Only log critical errors
	logFFmpeg := false

	cmd := exec.Command(
		"ffmpeg",
		// "-loglevel", "debug", // comentado para no ralentizar por logging
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

	// Cancelar ffmpeg si el contexto se cancela
	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()

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
				break
			}
		}
	}()

	// Leer stdout y extraer JPEGs completos
	go func() {
		defer stdout.Close()
		buf := make([]byte, 0, 512*1024)
		jpegSOI := []byte{0xFF, 0xD8}
		jpegEOI := []byte{0xFF, 0xD9}
		tmp := make([]byte, 32*1024)
		for {
			n, rerr := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					start := bytes.Index(buf, jpegSOI)
					if start < 0 {
						if len(buf) > 2*1024*1024 {
							buf = buf[:0]
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
					if onFrame != nil {
						onFrame(frame)
					}
					buf = buf[end:]
				}
			}
			if rerr != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

// RunFFmpegToMJPEG lanza ffmpeg para leer rtp-forwarder.sdp y emite una corriente
// de JPEGs por stdout (image2pipe / mjpeg). Cada frame JPEG completo se pasa al callback onFrame.
// Esto permite que MJPEG reciba frames incluso si no hay clientes aún.
func RunFFmpegToMJPEGFile(onFrame func([]byte)) error {
	logFFmpeg := false

	cmd := exec.Command(
		"ffmpeg",
		"-nostdin",
		"-protocol_whitelist", "file,udp,rtp",
		"-i", "rtp-forwarder.sdp",
		"-an",
		"-vf", "scale=-1:-1",
		"-c:v", "mjpeg",
		"-q:v", "8",
		"-f", "mjpeg",
		"pipe:1",
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

	outFile, err := os.Create("output.mjpeg")
	if err != nil {
		log.Printf("[FFmpeg] Error creando archivo output.mjpeg: %v", err)
		return err
	}
	defer outFile.Close()

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
				break
			}
		}
	}()

	// Leer stdout y escribir directamente en el archivo output.mjpeg (síncrono)
	defer stdout.Close()
	buf := make([]byte, 32*1024)
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, werr := outFile.Write(buf[:n]); werr != nil {
				log.Printf("[FFmpeg] Error escribiendo en output.mjpeg: %v", werr)
				break
			}
		}
		if rerr != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}
