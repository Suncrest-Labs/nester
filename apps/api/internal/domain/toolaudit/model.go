package toolaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type ToolInvocation struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id"`
	RequestID      string          `json:"request_id"`
	ConversationID string          `json:"conversation_id"`
	ToolName       string          `json:"tool_name"`
	Arguments      json.RawMessage `json:"arguments"`
	Consequential  bool            `json:"consequential"`
	Status         string          `json:"status"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	PrevHash       string          `json:"prev_hash"`
	EntryHash      string          `json:"entry_hash"`
	CreatedAt      time.Time       `json:"created_at"`
}

type toolInvocationForHash struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id"`
	RequestID      string          `json:"request_id"`
	ConversationID string          `json:"conversation_id"`
	ToolName       string          `json:"tool_name"`
	Arguments      json.RawMessage `json:"arguments"`
	Consequential  bool            `json:"consequential"`
	Status         string          `json:"status"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	CreatedAt      int64           `json:"created_at"` // Unix timestamp
}

func (t *ToolInvocation) ComputeHash(prevHash string) string {
	forHash := toolInvocationForHash{
		ID:             t.ID,
		UserID:         t.UserID,
		RequestID:      t.RequestID,
		ConversationID: t.ConversationID,
		ToolName:       t.ToolName,
		Arguments:      t.Arguments,
		Consequential:  t.Consequential,
		Status:         t.Status,
		Result:         t.Result,
		ErrorMessage:   t.ErrorMessage,
		CreatedAt:      t.CreatedAt.Unix(),
	}

	b, _ := json.Marshal(forHash)
	
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(b)
	
	return hex.EncodeToString(h.Sum(nil))
}
