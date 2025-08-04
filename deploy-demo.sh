#!/bin/bash

# Variables
EC2_USER=""          # The user on the EC2 instance
EC2_IP=""            # The public IP address of your EC2 instance
KEY_FILE=""          # The path to your SSH key file
BINARY_NAME=""       # The name of the compiled binary
ENV_FILE=""          # Path to the environment file

# Load environment variables from .env.deploy
if [ -f "$ENV_FILE" ]; then
    export $(grep -v '^#' $ENV_FILE | xargs)
else
    echo "Environment file $ENV_FILE not found."
    exit 1
fi

# Compile Go code
echo "Compiling Go code..."
GOOS=linux GOARCH=amd64 go build -o $BINARY_NAME cmd/server/main.go

if [ $? -ne 0 ]; then
    echo "Failed to compile Go code."
    exit 1
fi

# Transfer the binary to the EC2 instance
echo "Transferring binary to EC2 instance..."
scp -i $KEY_FILE $BINARY_NAME $EC2_USER@$EC2_IP:/home/$EC2_USER/

if [ $? -ne 0 ]; then
    echo "Failed to transfer binary to EC2 instance."
    rm $BINARY_NAME
    exit 1
fi

# Clean up
rm $BINARY_NAME

echo "Deployment completed successfully."