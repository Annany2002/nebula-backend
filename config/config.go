// config/config.go
package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/joho/godotenv"
)

var (
	customLog = logger.NewLogger()
)

// Config holds application configuration values
type Config struct {
	ServerPort     string
	JWTSecret      string
	JWTExpiration  time.Duration
	MetadataDbDir  string
	MetadataDbFile string
}

// LoadConfig loads configuration from environment variables.
// It uses a .env file for local development if present.
func LoadConfig() (*Config, error) {
	customLog.Println("Loading configuration from environment variables...")

	// Attempt to load .env file, ignore if not found (useful for production)
	err := godotenv.Load() // Loads .env file from current directory by default
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		customLog.Warnf("Warning: Error loading .env file: %v", err)
		// Decide if this should be a fatal error or just a warning
	}

	// Read values from environment variables, providing defaults where appropriate
	port := getEnv("SERVER_PORT", ":8080")                 // Default to :8080
	jwtSecret := getEnv("JWT_SECRET", "")                  // No sensible default for secret!
	jwtExpHoursStr := getEnv("JWT_EXPIRATION_HOURS", "24") // Default to 24 hours
	dbDir := getEnv("DATABASE_DIRECTORY", "data")
	dbFile := getEnv("DATABASE_DIRECTORY_FILE", "metadata.db")
	// --- Validation and Parsing ---

	// Critical: Ensure JWT Secret is set
	if jwtSecret == "" {
		return nil, errors.New("JWT_SECRET environment variable must be set")
	}
	if jwtSecret == "!!replace_this_with_a_real_secret_key!!" {
		customLog.Warnln("WARNING: JWT_SECRET is set to the default placeholder!")
		// return nil, errors.New("default placeholder JWT_SECRET must be changed")
	}

	// Parse JWT Expiration
	jwtExpHours, err := strconv.Atoi(jwtExpHoursStr)
	if err != nil || jwtExpHours <= 0 {
		customLog.Warnf("Warning: Invalid JWT_EXPIRATION_HOURS '%s'. Using default 24h. Error: %v", jwtExpHoursStr, err)
		jwtExpHours = 24 // Fallback to default on parsing error
	}
	jwtExpiration := time.Hour * time.Duration(jwtExpHours)

	cfg := &Config{
		ServerPort:     port,
		JWTSecret:      jwtSecret,
		JWTExpiration:  jwtExpiration,
		MetadataDbDir:  dbDir,
		MetadataDbFile: dbFile,
	}

	customLog.Printf("Configuration loaded successfully. Port: %s, JWT Exp: %v", cfg.ServerPort, cfg.JWTExpiration)
	return cfg, nil
}

// getEnv reads an environment variable or returns a default value.
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
