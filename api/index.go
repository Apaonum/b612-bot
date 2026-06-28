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

type Emoji struct {
	Name string `json:"name,omitempty"`
}

type Component struct {
	Type        int    `json:"type"`
	Style       int    `json:"style,omitempty"`
	Label       string `json:"label,omitempty"`
	CustomID    string `json:"custom_id,omitempty"`
	Emoji       *Emoji `json:"emoji,omitempty"`
	MinLength   int    `json:"min_length,omitempty"`
	MaxLength   int    `json:"max_length,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type ActionRow struct {
	Type       int         `json:"type"`
	Components []Component `json:"components"`
}

type Interaction struct {
	Type    int    `json:"type"`
	GuildID string `json:"guild_id"` // Role config
	Member  struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name     string `json:"name,omitempty"`
		CustomID string `json:"custom_id,omitempty"`
		Options  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options,omitempty"`
		// Components structure from modal
		Components []struct {
			Type       int `json:"type"`
			Components []struct {
				Type     int    `json:"type"`
				CustomID string `json:"custom_id"`
				Value    string `json:"value"`
			} `json:"components"`
		} `json:"components,omitempty"`
	} `json:"data"`
}

type InteractionResponse struct {
	Type int                      `json:"type"`
	Data *InteractionResponseData `json:"data,omitempty"`
}

type InteractionResponseData struct {
	Content    string      `json:"content"`
	Flags      int         `json:"flags,omitempty"`
	Title      string      `json:"title,omitempty"`
	CustomID   string      `json:"custom_id,omitempty"`
	Components []ActionRow `json:"components,omitempty"`
}

func generateB612Code() string {
	b := make([]byte, 3)
	rand.Read(b)
	return "B612-" + hex.EncodeToString(b)
}

func assignRoleToUser(guildID, userID, roleID string) error {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s/roles/%s", guildID, userID, roleID)

	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to assign role, status code: %d", resp.StatusCode)
	}
	return nil
}

func saveCodeToSupabase(code string, roleID string) error {
	url := os.Getenv("SUPABASE_URL") + "/rest/v1/invite_codes"
	payload := fmt.Sprintf(`{"code":"%s", "role_id":"%s", "is_used":false}`, code, roleID)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer([]byte(payload)))
	req.Header.Set("apikey", os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("supabase error: %d", resp.StatusCode)
	}
	return nil
}

func redeemCodeFromSupabase(code string) (string, error) {
	// ค้นหาโค้ดที่ตรงกันและยังไม่ถูกใช้ (is_used=eq.false)
	url := os.Getenv("SUPABASE_URL") + "/rest/v1/invite_codes?code=eq." + code + "&is_used=eq.false&select=role_id"
	payload := `{"is_used":true}`

	req, _ := http.NewRequest("PATCH", url, bytes.NewBuffer([]byte(payload)))
	req.Header.Set("apikey", os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Content-Type", "application/json")
	// บังคับให้ Supabase คืนค่า row ที่ถูกอัปเดตกลับมา
	req.Header.Set("Prefer", "return=representation")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// แปลง JSON Array ที่ตอบกลับมา
	var result []struct {
		RoleID string `json:"role_id"`
	}
	json.Unmarshal(body, &result)

	// ถ้า array ว่าง แปลว่าหาโค้ดไม่เจอ หรือถูกใช้ไปแล้ว
	if len(result) == 0 {
		return "", fmt.Errorf("invalid or already used code")
	}
	return result[0].RoleID, nil
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

	// Generate Code command
	if interaction.Type == 2 {
		if interaction.Data.Name == "generate-code" {
			// Pull Role ID from Admin selected
			var roleID string
			if len(interaction.Data.Options) > 0 {
				roleID = interaction.Data.Options[0].Value
			}

			// สุ่มโค้ดใหม่
			newCode := generateB612Code()

			err := saveCodeToSupabase(newCode, roleID)
			var responseMsg string
			if err != nil {
				responseMsg = "❌ เกิดข้อผิดพลาดในการบันทึกรหัสลงฐานข้อมูล"
			} else {
				responseMsg = fmt.Sprintf("🎟️ สร้าง 1-Time Code สำเร็จ!\nCode: **`%s`**\nผูกกับ Role: <@&%s>\n*(คัดลอกโค้ดนี้ส่งให้ User เพื่อนำไป Redeem ได้เลย)*", newCode, roleID)
			}

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

		// Setup Welcome command
		if interaction.Data.Name == "setup-welcome" {
			btn := Component{
				Type:     2,
				Style:    1,
				Label:    "Redeem Code",
				CustomID: "btn_redeem_code",
				Emoji: &Emoji{
					Name: "🎟️",
				},
			}

			row := ActionRow{
				Type:       1,
				Components: []Component{btn},
			}

			responseMsg := "ยินดีต้อนรับสู่เซิร์ฟเวอร์! 👋\nหากคุณมี 1-Time Code (B612-xxxxxx) สำหรับรับ Role พิเศษ สามารถกดปุ่มด้านล่างเพื่อกรอกรหัสได้เลยครับ"

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(InteractionResponse{
				Type: 4,
				Data: &InteractionResponseData{
					Content:    responseMsg,
					Components: []ActionRow{row},
				},
			})
			return
		}
	}

	if interaction.Type == 3 {
		if interaction.Data.CustomID == "btn_redeem_code" {

			textInput := Component{
				Type:        4, // 4 = Text Input
				CustomID:    "input_b612_code",
				Style:       1, // 1 = Short text (1 line text)
				Label:       "กรุณากรอกรหัส B612",
				Placeholder: "เช่น B612-a1b2c3",
				MinLength:   11, // validate (B612- + 6 char)
				MaxLength:   11,
				Required:    true,
			}

			row := ActionRow{
				Type:       1,
				Components: []Component{textInput},
			}

			// Response in Type 9 = Modal (Pop-up modal)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(InteractionResponse{
				Type: 9,
				Data: &InteractionResponseData{
					CustomID:   "modal_submit_code",
					Title:      "🎟️ Redeem Role",
					Components: []ActionRow{row},
				},
			})
			return
		}
	}

	if interaction.Type == 5 {
		if interaction.Data.CustomID == "modal_submit_code" {
			userCode := ""
			if len(interaction.Data.Components) > 0 && len(interaction.Data.Components[0].Components) > 0 {
				userCode = interaction.Data.Components[0].Components[0].Value
			}

			// fetch user data and server
			userID := interaction.Member.User.ID
			guildID := interaction.GuildID

			roleID, err := redeemCodeFromSupabase(userCode)

			var resultMessage string
			if err != nil {
				// Wrong Code, Already used code
				resultMessage = fmt.Sprintf("❌ รหัส **`%s`** ไม่ถูกต้อง หรืออาจจะถูกใช้งานไปแล้วครับ", userCode)
			} else {
				// Right and new code -> Use RoleID fron DB to append in Add Role
				assignErr := assignRoleToUser(guildID, userID, roleID)
				if assignErr != nil {
					resultMessage = fmt.Sprintf("❌ โค้ดถูกต้อง แต่บอทไม่สามารถให้ Role ได้: %v", assignErr)
				} else {
					resultMessage = fmt.Sprintf("🎉 ยินดีด้วยครับ! รหัส **`%s`** ถูกต้อง คุณได้รับ Role เรียบร้อยแล้ว!", userCode)
				}
			}

			// Response to user by Ephemeral
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(InteractionResponse{
				Type: 4,
				Data: &InteractionResponseData{
					Content: resultMessage,
					Flags:   64,
				},
			})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}
