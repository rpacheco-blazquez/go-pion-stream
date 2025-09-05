package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go-pion-stream-1/internal/config"
	"go-pion-stream-1/internal/webrtc"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"
)

// Estructura para analizar la respuesta JSON de ngrok
type Tunnel struct {
	PublicURL string `json:"public_url"`
}
type TunnelListResource struct {
	Tunnels []Tunnel `json:"tunnels"`
}

// Función para obtener la URL pública de ngrok
func getNgrokPublicURL() string {
	resp, err := http.Get("http://127.0.0.1:4040/api/tunnels")
	if err != nil {
		log.Printf("[ngrok] Error al realizar la solicitud HTTP: %v", err)
		return ""
	}
	defer resp.Body.Close()

	// Leer el cuerpo de la respuesta
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[ngrok] Error al leer el cuerpo de la respuesta: %v", err)
		return ""
	}

	// Registrar el contenido del JSON en los logs
	log.Printf("[ngrok] Respuesta JSON recibida: %s", string(body))

	// Intentar analizar el JSON
	var result struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[ngrok] Error al analizar la respuesta JSON: %v", err)
		return ""
	}

	if len(result.Tunnels) > 0 {
		return result.Tunnels[0].PublicURL
	}
	return ""
}

// Función para obtener la URL pública de ngrok con reintentos
func getNgrokPublicURLWithRetries(maxRetries int, delay time.Duration) string {
	for i := 0; i < maxRetries; i++ {
		publicURL := getNgrokPublicURL()
		if publicURL != "" {
			return publicURL
		}
		log.Printf("[ngrok] Intento %d: No se pudo obtener la URL pública, reintentando en %v...", i+1, delay)
		time.Sleep(delay)
		delay *= 2 // Incrementar el delay exponencialmente
	}
	return ""
}

func main() {
	log.SetOutput(io.Discard)

	cfg := config.Load()

	// Abrir o truncar el archivo de log
	logFile, err := os.OpenFile("webrtc_server.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("No se pudo crear el archivo de log:", err)
		os.Exit(1)
	}
	log.SetOutput(logFile)
	fmt.Println("Go Pion Stream Server started on port", cfg.Port)
	log.Println("[LOG] Todos los logs se guardarán en webrtc_server.log")

	// Iniciar solo el servidor WebRTC
	// Iniciar ngrok como un proceso paralelo
	cmd := exec.Command(".\\ngrok.exe", "http", fmt.Sprintf("%d", cfg.Port))
	// Usar un buffer para capturar la salida de ngrok
	var ngrokOutput bytes.Buffer
	cmd.Stdout = &ngrokOutput
	cmd.Stderr = logFile

	// Iniciar ngrok y capturar su salida
	if err := cmd.Start(); err != nil {
		log.Fatalf("Error al iniciar ngrok: %v", err)
	}

	// Agregar un delay antes de obtener la URL pública de ngrok
	time.Sleep(2 * time.Second)

	// Obtener y registrar la URL pública de ngrok después del delay
	publicURL := getNgrokPublicURLWithRetries(5, 2*time.Second)
	if publicURL != "" {
		log.Printf("[ngrok] URL pública: %s", publicURL)
	} else {
		log.Printf("[ngrok] No se pudo obtener la URL pública después de varios intentos")
	}

	// Cambiar la ruta de guardado del QR a static/QR.png
	qrFilePath := "static/QR.png"
	if publicURL != "" {
		qrURL := publicURL + "/streamui"
		log.Printf("[ngrok] Generando código QR para la URL: %s", qrURL)
		if err := qrcode.WriteFile(qrURL, qrcode.Medium, 256, qrFilePath); err != nil {
			log.Printf("[QR] Error al generar el código QR: %v", err)
		} else {
			log.Printf("[QR] Código QR generado y guardado como %s", qrFilePath)
		}
	}

	// Asegurar que el archivo QR se elimine al finalizar el programa
	defer func() {
		if err := os.Remove(qrFilePath); err != nil {
			log.Printf("[QR] Error al eliminar el archivo QR: %v", err)
		} else {
			log.Printf("[QR] Archivo QR eliminado correctamente")
		}
	}()

	// Manejar señales del sistema para eliminar el archivo QR al detener el programa
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("[Server] Señal de interrupción recibida, limpiando recursos...")
		if err := os.Remove("static/QR.png"); err != nil {
			log.Printf("[QR] Error al eliminar el archivo QR: %v", err)
		} else {
			log.Printf("[QR] Archivo QR eliminado correctamente")
		}
		os.Exit(0)
	}()

	// Configurar el servidor para servir archivos estáticos desde el directorio "static"
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Iniciar el servidor WebRTC
	webrtc.StartWebRTCServer(cfg.Port)

	// Esperar a que ngrok termine
	if err := cmd.Wait(); err != nil {
		log.Printf("ngrok terminó con error: %v", err)
	}
}
