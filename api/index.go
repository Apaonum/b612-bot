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

// ==========================================
// 1. Structs
// ==========================================

type Emoji struct {
	Name string `json:"name,omitempty"`
}

type SelectOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Emoji       *Emoji `json:"emoji,omitempty"`
}

type Component struct {
	Type        int            `json:"type"`
	Style       int            `json:"style,omitempty"`
	Label       string         `json:"label,omitempty"`
	CustomID    string         `json:"custom_id,omitempty"`
	Emoji       *Emoji         `json:"emoji,omitempty"`
	MinLength   int            `json:"min_length,omitempty"`
	MaxLength   int            `json:"max_length,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Options     []SelectOption `json:"options,omitempty"`
}

type ActionRow struct {
	Type       int         `json:"type"`
	Components []Component `json:"components"`
}

type Interaction struct {
	Type    int    `json:"type"`
	GuildID string `json:"guild_id"`
	Member  struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name     string   `json:"name,omitempty"`
		CustomID string   `json:"custom_id,omitempty"`
		Values   []string `json:"values,omitempty"`
		Options  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options,omitempty"`
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
	Content    string      `json:"content,omitempty"`
	Flags      int         `json:"flags,omitempty"`
	Title      string      `json:"title,omitempty"`
	CustomID   string      `json:"custom_id,omitempty"`
	Components []ActionRow `json:"components,omitempty"`
}

// ==========================================
// 2. Database & API Helpers
// ==========================================

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
	url := os.Getenv("SUPABASE_URL") + "/rest/v1/invite_codes?code=eq." + code + "&is_used=eq.false&select=role_id"
	payload := `{"is_used":true}`

	req, _ := http.NewRequest("PATCH", url, bytes.NewBuffer([]byte(payload)))
	req.Header.Set("apikey", os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("SUPABASE_KEY"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result []struct {
		RoleID string `json:"role_id"`
	}
	json.Unmarshal(body, &result)

	if len(result) == 0 {
		return "", fmt.Errorf("invalid or already used code")
	}
	return result[0].RoleID, nil
}

// ตัวช่วยสำหรับส่ง JSON Response เพื่อลดโค้ดที่ซ้ำซ้อน
func sendJSONResponse(w http.ResponseWriter, resp InteractionResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ==========================================
// 3. Extracted Event Handlers
// ==========================================

func handleGenerateCode(w http.ResponseWriter, interaction Interaction) {
	var roleID string
	if len(interaction.Data.Options) > 0 {
		roleID = interaction.Data.Options[0].Value
	}

	newCode := generateB612Code()
	err := saveCodeToSupabase(newCode, roleID)

	var responseMsg string
	if err != nil {
		responseMsg = "❌ เกิดข้อผิดพลาดในการบันทึกรหัสลงฐานข้อมูล"
	} else {
		responseMsg = fmt.Sprintf("🎟️ สร้าง 1-Time Code สำเร็จ!\nCode: **`%s`**\nผูกกับ Role: <@&%s>\n*(คัดลอกโค้ดนี้ส่งให้ User เพื่อนำไป Redeem ได้เลย)*", newCode, roleID)
	}

	sendJSONResponse(w, InteractionResponse{
		Type: 4,
		Data: &InteractionResponseData{
			Content: responseMsg,
			Flags:   64,
		},
	})
}

func handleSetupWelcome(w http.ResponseWriter) {
	dropdown := Component{
		Type:        3,
		CustomID:    "select_basic_role",
		Placeholder: "คลิกเพื่อเลือก Role พื้นฐานของคุณ...",
		Options: []SelectOption{
			{
				Label:       "Guest",
				Value:       "889899263843258389", // TODO: เปลี่ยนเป็น Role ID จริง
				Description: "For every new entries guys",
				Emoji:       &Emoji{Name: "👤"},
			},
		},
	}

	btn := Component{
		Type:     2,
		Style:    1,
		Label:    "Redeem Code",
		CustomID: "btn_redeem_code",
		Emoji:    &Emoji{Name: "🎟️"},
	}

	row1 := ActionRow{Type: 1, Components: []Component{dropdown}}
	row2 := ActionRow{Type: 1, Components: []Component{btn}}

	responseMsg := "ยินดีต้อนรับสู่เซิร์ฟเวอร์! 👋\nกรุณาเลือก Role พื้นฐานของคุณจากเมนูด้านล่าง หรือหากคุณมี 1-Time Code สามารถกดปุ่มเพื่อ Redeem รับ Role พิเศษได้เลยครับ"

	sendJSONResponse(w, InteractionResponse{
		Type: 4,
		Data: &InteractionResponseData{
			Content:    responseMsg,
			Components: []ActionRow{row1, row2},
		},
	})
}

func handleRedeemClick(w http.ResponseWriter) {
	textInput := Component{
		Type:        4,
		CustomID:    "input_b612_code",
		Style:       1,
		Label:       "กรุณากรอกรหัส B612",
		Placeholder: "เช่น B612-a1b2c3",
		MinLength:   11,
		MaxLength:   11,
		Required:    true,
	}

	row := ActionRow{
		Type:       1,
		Components: []Component{textInput},
	}

	sendJSONResponse(w, InteractionResponse{
		Type: 9, // Modal
		Data: &InteractionResponseData{
			CustomID:   "modal_submit_code",
			Title:      "🎟️ Redeem Role",
			Components: []ActionRow{row},
		},
	})
}

func handleModalSubmit(w http.ResponseWriter, interaction Interaction) {
	userCode := ""
	if len(interaction.Data.Components) > 0 && len(interaction.Data.Components[0].Components) > 0 {
		userCode = interaction.Data.Components[0].Components[0].Value
	}

	userID := interaction.Member.User.ID
	guildID := interaction.GuildID

	roleID, err := redeemCodeFromSupabase(userCode)
	var resultMessage string

	if err != nil {
		resultMessage = fmt.Sprintf("❌ รหัส **`%s`** ไม่ถูกต้อง หรืออาจจะถูกใช้งานไปแล้วครับ", userCode)
	} else {
		assignErr := assignRoleToUser(guildID, userID, roleID)
		if assignErr != nil {
			resultMessage = fmt.Sprintf("❌ โค้ดถูกต้อง แต่บอทไม่สามารถให้ Role ได้: %v", assignErr)
		} else {
			resultMessage = fmt.Sprintf("🎉 ยินดีด้วยครับ! รหัส **`%s`** ถูกต้อง คุณได้รับ Role เรียบร้อยแล้ว!", userCode)
		}
	}

	sendJSONResponse(w, InteractionResponse{
		Type: 4,
		Data: &InteractionResponseData{
			Content: resultMessage,
			Flags:   64,
		},
	})
}

func handleRoleSelect(w http.ResponseWriter, interaction Interaction) {
	if len(interaction.Data.Values) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	selectedRoleID := interaction.Data.Values[0]
	userID := interaction.Member.User.ID
	guildID := interaction.GuildID

	err := assignRoleToUser(guildID, userID, selectedRoleID)

	var resultMessage string
	if err != nil {
		resultMessage = fmt.Sprintf("❌ บอทไม่สามารถให้ Role ได้: %v", err)
	} else {
		resultMessage = fmt.Sprintf("✅ คุณได้รับ Role <@&%s> เรียบร้อยแล้วครับ!", selectedRoleID)
	}

	sendJSONResponse(w, InteractionResponse{
		Type: 4,
		Data: &InteractionResponseData{
			Content: resultMessage,
			Flags:   64,
		},
	})
}

// ==========================================
// 4. Main Gateway (Routing)
// ==========================================

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

	// Routing ตามประเภทของ Interaction
	switch interaction.Type {
	case 1: // PING
		sendJSONResponse(w, InteractionResponse{Type: 1})
		return
	case 2: // Slash Commands
		if interaction.Data.Name == "generate-code" {
			handleGenerateCode(w, interaction)
			return
		}
		if interaction.Data.Name == "setup-welcome" {
			handleSetupWelcome(w)
			return
		}
	case 3: // Message Components (Button Click)
		if interaction.Data.CustomID == "btn_redeem_code" {
			handleRedeemClick(w)
			return
		}
		if interaction.Data.CustomID == "select_basic_role" {
			handleRoleSelect(w, interaction)
			return
		}
	case 5: // Modal Submit
		if interaction.Data.CustomID == "modal_submit_code" {
			handleModalSubmit(w, interaction)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}
