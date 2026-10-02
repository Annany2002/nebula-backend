package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/Annany2002/nebula-backend/internal/logger"
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
// It uses a .env file for local development if present (ignores it for production).
func LoadConfig() (*Config, error) {
	customLog.Println("Loading configuration from environment variables...")

	// Attempt to load .env file if in development environment (skip in production)
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			customLog.Warnf("Warning: Error loading .env file: %v", err)
		}
	}

	// Read values from environment variables, providing defaults where appropriate
	defaultPort := "8080"
	if os.Getenv("APP_ENV") == "production" {
		defaultPort = "8085"
	}
	port := strings.TrimPrefix(getEnv("SERVER_PORT", defaultPort), ":")
	jwtSecret := getEnv("JWT_SECRET", "")                  // No sensible default for secret!
	jwtExpHoursStr := getEnv("JWT_EXPIRATION_HOURS", "24") // Default to 24 hours
	dbDir := getEnv("DATABASE_DIRECTORY", "data")
	dbFile := getEnv("DATABASE_DIRECTORY_FILE", "metadata.db")

	// --- Validation and Parsing ---
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("SERVER_PORT must be a port number between 1 and 65535")
	}
	// Critical: Ensure JWT Secret is set
	if jwtSecret == "" {
		return nil, errors.New("JWT_SECRET environment variable must be set")
	}
	if jwtSecret == "!!replace_this_with_a_real_secret_key!!" {
		customLog.Warnln("WARNING: JWT_SECRET is set to the default placeholder!")
	}

	// Parse JWT Expiration (hours)
	jwtExpHours, err := strconv.Atoi(jwtExpHoursStr)
	if err != nil || jwtExpHours <= 0 {
		customLog.Warnf("Invalid JWT_EXPIRATION_HOURS '%s'. Using default 24h. Error: %v", jwtExpHoursStr, err)
		jwtExpHours = 24 // Default to 24 hours
	}
	jwtExpiration := time.Hour * time.Duration(jwtExpHours)

	// Return final Config struct
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
