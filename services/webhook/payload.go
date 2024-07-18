package services

type Payload struct {
	Content     *string    `json:"content"`
	Embeds      *[]Embed   `json:"embeds"`
	Attachments []struct{} `json:"attachments"`
}

// NewPayload creates a new Payload object
func newPayload() *Payload {
	return &Payload{}
}

// SetContent sets the content of the payload (raw msg data)
func (w *Payload) SetContent(content string) (webhook *Payload) {
	w.Content = &content
	return w
}

// SetEmbeds sets the embeds of the payload
func (w *Payload) SetEmbeds(embeds *[]Embed) (webhook *Payload) {
	w.Embeds = embeds
	return w
}
