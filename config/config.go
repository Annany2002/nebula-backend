// config/config.go
package config

import (
	"errors"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Config holds application configuration values
type Config struct {
	ServerPort     string
	JWTSecret      string
	JWTExpiration  time.Duration
	MetadataDbDir  string
	MetadataDbFile string
	// Add other config fields as needed
}

// LoadConfig loads configuration.
func LoadConfig() (*Config, error) {
	err := godotenv.Load("../../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	log.Println("Loading configuration")

	port, secret, db_dir, db_dir_file := os.Getenv("PORT"), os.Getenv("JWT_SECRET"), os.Getenv("DATABASE_DIRECTORY"), os.Getenv("DATABASE_DIRECTORY_FILE")

	cfg := &Config{
		ServerPort:     port,
		JWTSecret:      secret,
		JWTExpiration:  time.Hour * 24,
		MetadataDbDir:  db_dir,
		MetadataDbFile: db_dir_file,
	}

	// Basic validation (e.g., ensure JWT secret is not the placeholder)
	if cfg.JWTSecret == "!!replace_this_with_a_real_secret_key!!" {
		log.Println("WARNING: JWT Secret is set to the default placeholder!")
		return nil, errors.New("JWT_SECRET must be set")
	}

	log.Println("Configuration loaded.")
	return cfg, nil
}
