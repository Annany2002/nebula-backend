package main

import "time"

// !! IMPORTANT: Store this securely (e.g., env var) in production !!
var jwtSecretKey = []byte("!!replace_this_with_a_real_secret_key!!")

const jwtExpiration = time.Hour * 24 // Token valid for 24 hours
const metadataDbDir = "data"         // Directory for DB files
const metadataDbFile = "metadata.db" // Name of the metadata DB file
