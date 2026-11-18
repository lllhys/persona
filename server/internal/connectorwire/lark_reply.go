package connectorwire

import "time"

// LarkReplyWatch is an explicit source identity, not a person's display name or a model's Inbox association.
type LarkReplyWatch struct {
	Source           string `json:"source"`
	Brand            string `json:"brand"`
	AppID            string `json:"app_id"`
	AccountOpenID    string `json:"account_open_id"`
	PersonOpenID     string `json:"person_open_id"`
	ChatID           string `json:"chat_id"`
	ThreadID         string `json:"thread_id"`
	RequestMessageID string `json:"request_message_id"`
}

// LarkReplyMessage keeps only source identity and time, never conversation text. Before is the exclusive upper
// bound of the source timestamp's precision; an ambiguous minute cannot prove ordering within that minute.
type LarkReplyMessage struct {
	ID         string    `json:"id"`
	SenderID   string    `json:"sender_id"`
	SenderType string    `json:"sender_type"`
	IDType     string    `json:"id_type"`
	AppID      string    `json:"app_id,omitempty"`
	ChatID     string    `json:"chat_id"`
	ThreadID   string    `json:"thread_id"`
	URL        string    `json:"url,omitempty"`
	At         time.Time `json:"at"`
	Before     time.Time `json:"before"`
}

// LarkReplyObservation is the bounded reader's evidence. Watch echoes the identity actually checked by the source,
// so a peer ignoring a new request field cannot accidentally satisfy a watch. No request or reply is a no-match.
type LarkReplyObservation struct {
	Watch     LarkReplyWatch   `json:"watch"`
	Request   LarkReplyMessage `json:"request"`
	Reply     LarkReplyMessage `json:"reply"`
	CheckedAt time.Time        `json:"checked_at"`
}
