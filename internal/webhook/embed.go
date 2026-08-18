package webhook

import (
	"encoding/json"
	"time"
)

type Embed struct {
	Title       *string      `json:"title,omitempty"`
	Description *string      `json:"description,omitempty"`
	Color       *int         `json:"color"`
	Timestamp   *string      `json:"timestamp,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

// NewEmbed creates a new Embed object
func NewEmbed() *Embed {
	return &Embed{}
}

// SetTitle sets the title of the embed
func (e *Embed) SetTitle(title string) (embed *Embed) {
	e.Title = &title
	return e
}

// SetDescription sets the description of the embed
func (e *Embed) SetDescription(description string) (embed *Embed) {
	e.Description = &description
	return e
}

// SetColor sets the color of the embed
func (e *Embed) SetColor(color int) (embed *Embed) {
	e.Color = &color
	return e
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// AddField adds a field to the embed
func (e *Embed) AddField(data EmbedField) *Embed {
	e.Fields = append(e.Fields, EmbedField{Name: data.Name, Value: data.Value, Inline: data.Inline})
	return e
}

// AddTimestamp adds a timestamp field to the embed
func (e *Embed) SetTimestamp() *Embed {
	now := time.Now().UTC()
	formattedTime := now.Format("2006-01-02T15:04:05.000Z")

	e.Timestamp = &formattedTime

	return e
}

// // AddConInfo adds connection information to the embed
// func (e *Embed) AddConInfo(conInfo *structs.ConnectionInfo) *Embed {
// 	e.AddField(EmbedField{Name: "Session ID", Value: conInfo.SeshId, Inline: true})
// 	e.AddField(EmbedField{Name: "License Key", Value: conInfo.LicenseKey, Inline: true})
// 	e.AddField(EmbedField{Name: "HWID", Value: conInfo.Hwid, Inline: true})
// 	e.AddField(EmbedField{Name: "IP", Value: conInfo.Ip, Inline: true})

// 	return e
// }

// // AddUserInfo adds user information to the embed
// func (e *Embed) AddUserInfo(userInfo *structs.UserInfo) *Embed {
// 	if userInfo == nil {
// 		return e
// 	}

// 	e.AddField(EmbedField{Name: "License ID", Value: userInfo.LicenseId, Inline: true})
// 	e.AddField(EmbedField{Name: "Discord ID", Value: fmt.Sprintf("<@%v>", userInfo.DiscordId), Inline: true})

// 	return e
// }

// ExportToJSON converts the embed to a JSON string
func (e *Embed) ExportToJSON() (string, error) {
	jsonData, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}
