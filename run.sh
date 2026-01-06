#!/bin/bash

# Nebula Backend - Local Run Script
# This script builds and runs the Nebula backend server locally

set -e  # Exit on error

# Variables
BINARY_NAME="nebula-backend-server"
ENV_FILE=".env"
BUILD_DIR="./bin"
LOG_DIR="./logs"
DATA_DIR="./data"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Nebula Backend - Local Run Script${NC}"
echo -e "${GREEN}========================================${NC}"

# Check if .env file exists
if [ ! -f "$ENV_FILE" ]; then
    echo -e "${RED}Error: Environment file $ENV_FILE not found.${NC}"
    echo "Please create a .env file with required configuration."
    exit 1
fi

echo -e "${YELLOW}Loading environment variables from $ENV_FILE...${NC}"
export $(grep -v '^#' $ENV_FILE | xargs)

# Create necessary directories
echo -e "${YELLOW}Creating necessary directories...${NC}"
mkdir -p $BUILD_DIR $LOG_DIR $DATA_DIR

# Clean previous build
if [ -f "$BUILD_DIR/$BINARY_NAME" ]; then
    echo -e "${YELLOW}Cleaning previous build...${NC}"
    rm $BUILD_DIR/$BINARY_NAME
fi

# Build the Go application
echo -e "${YELLOW}Building Go application...${NC}"
CGO_ENABLED=1 go build -ldflags="-s -w" -o $BUILD_DIR/$BINARY_NAME ./cmd/server/main.go

if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to build Go application.${NC}"
    exit 1
fi

echo -e "${GREEN}Build successful!${NC}"

# Run the application
echo -e "${GREEN}Starting Nebula Backend server...${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop the server${NC}"
echo -e "${GREEN}========================================${NC}"

./$BUILD_DIR/$BINARY_NAME
