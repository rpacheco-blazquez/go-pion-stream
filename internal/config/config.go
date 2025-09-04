package config

type Config struct {
	Port int
}

func Load() Config {
	return Config{
		Port: 8080,
	}
}
