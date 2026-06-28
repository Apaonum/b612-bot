package handler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

type Interaction struct {
	Type int `json:"type"`
}

type InteractionResponse struct {
	Type int `json:"type"`
}

// Serverless fucntion handler
func Handler(w http.ResponseWriter, r *http.Request) {
	pubKeyHex := os.Getenv("DISCORD_PUBLIC_KEY")
	pubKey, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		http.Error(w, "Invalid Public Key configuration", http.StatusInternalServerError)
		return
	}

	signatureHex := r.Header.Get("X-Signature-Ed25519")
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		http.Error(w, "Invalid Signature header", http.StatusUnauthorized)
		return
	}
	timestamp := r.Header.Get("X-Signature-Timestamp")

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Signature (Ed25519)
	message := []byte(timestamp)
	message = append(message, body...)

	if !ed25519.Verify(pubKey, message, signature) {
		http.Error(w, "Unauthorized signature", http.StatusUnauthorized)
		return
	}

	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InteractionResponse{Type: 1})
		return
	}

	// TODO: Slash Command Implementation under here

	w.WriteHeader(http.StatusOK)
}