package main

import (
	"fmt"
	"go-pion-stream-1/internal/config"
	"go-pion-stream-1/internal/webrtc"
	"log"
	"os"
)

func main() {
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
	webrtc.StartWebRTCServer(cfg.Port)
}
