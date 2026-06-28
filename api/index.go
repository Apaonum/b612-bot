package handler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Interaction struct {
	Type int `json:"type"`
	Data struct {
		Name    string `json:"name"`
		Options []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options"`
	} `json:"data"`
}

type InteractionResponse struct {
	Type int                      `json:"type"`
	Data *InteractionResponseData `json:"data,omitempty"`
}

type InteractionResponseData struct {
	Content string `json:"content"`
	Flags   int    `json:"flags,omitempty"`
}

func generateB612Code() string {
	b := make([]byte, 3)
	rand.Read(b)
	return "B612-" + hex.EncodeToString(b)
}

func Handler(w http.ResponseWriter, r *http.Request) {
	pubKeyHex := os.Getenv("DISCORD_PUBLIC_KEY")
	pubKey, _ := hex.DecodeString(pubKeyHex)

	signatureHex := r.Header.Get("X-Signature-Ed25519")
	signature, _ := hex.DecodeString(signatureHex)
	timestamp := r.Header.Get("X-Signature-Timestamp")

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	message := []byte(timestamp)
	message = append(message, body...)

	if !ed25519.Verify(pubKey, message, signature) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// PING control
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InteractionResponse{Type: 1})
		return
	}

	// Manage Slash Command
	if interaction.Type == 2 {
		if interaction.Data.Name == "generate-code" {
			// Pull Role ID from Admin selected
			var roleID string
			if len(interaction.Data.Options) > 0 {
				roleID = interaction.Data.Options[0].Value
			}

			// สุ่มโค้ดใหม่
			newCode := generateB612Code()
			
			// TODO: ในอนาคตเราจะเอา newCode กับ roleID ไป Save ลง Database ตรงนี้

			responseMsg := fmt.Sprintf("🎟️ สร้าง 1-Time Code สำเร็จ!\nCode: **`%s`**\nผูกกับ Role: <@&%s>\n*(คัดลอกโค้ดนี้ส่งให้ User เพื่อนำไป Redeem ได้เลย)*", newCode, roleID)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(InteractionResponse{
				Type: 4, // Type 4 = Immedietly response
				Data: &InteractionResponseData{
					Content: responseMsg,
					Flags:   64, // Flags 64 = Ephemeral
				},
			})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}