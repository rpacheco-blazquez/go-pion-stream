package config

import "os"

type Config struct {
	Port      int
	UDPBindIP string
}

// Global variable to store the ngrok public URL
var NgrokPublicURL string

// Getter for the ngrok public URL
func GetNgrokPublicURL() string {
	return NgrokPublicURL
}

func Load() Config {
	udpIP := os.Getenv("UDP_BIND_IP")
	if udpIP == "" {
		udpIP = "127.0.0.1"
	}
	return Config{
		Port:      8080,
		UDPBindIP: udpIP,
	}
}
