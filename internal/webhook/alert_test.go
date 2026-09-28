package webhook

import (
	"strings"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// embedText gathers every place an embed carries readable text, so a test can
// assert that a fact reached the alert without dictating which field holds it.
// Layout is the destination's business; presence is the segment's.
func embedText(e Embed) string {
	var b strings.Builder
	for _, p := range []*string{e.Title, e.URL, e.Description} {
		if p != nil {
			b.WriteString(*p)
			b.WriteString("\n")
		}
	}
	for _, f := range e.Fields {
		b.WriteString(f.Name)
		b.WriteString("\n")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}
	return b.String()
}

// @spec NOTIFY-ALERT-001, NOTIFY-ALERT-002, NOTIFY-ALERT-003, NOTIFY-DISCORD-002
func TestEmbedCarriesTheStoreProductAndVariant(t *testing.T) {
	embed := buildEmbed(testEvent(utils.Restock))
	text := embedText(embed)

	if embed.Title == nil || *embed.Title != "Test Sneaker" {
		t.Errorf("embed title = %v, want the product title", embed.Title)
	}
	if embed.URL == nil || *embed.URL != "https://kith.com/products/test-sneaker" {
		t.Errorf("embed url = %v, want a link to the product on its store", embed.URL)
	}
	if !strings.Contains(text, "kith.com") {
		t.Errorf("embed does not identify the store it came from:\n%s", text)
	}
	if !strings.Contains(text, "Size 10") {
		t.Errorf("embed does not name the variant:\n%s", text)
	}
	if !strings.Contains(text, "7") {
		t.Errorf("embed does not carry the variant identifier:\n%s", text)
	}
}

// The kind has to read before the text does.
//
// @spec NOTIFY-ALERT-004, NOTIFY-DISCORD-003
func TestEmbedDistinguishesARestockFromANewListing(t *testing.T) {
	restock := buildEmbed(testEvent(utils.Restock))
	listing := buildEmbed(testEvent(utils.NewVariant))

	if restock.Color == nil || listing.Color == nil {
		t.Fatalf("embeds carry no colour: restock=%v newVariant=%v", restock.Color, listing.Color)
	}
	if *restock.Color == *listing.Color {
		t.Errorf("both kinds render as colour %d; they cannot be told apart at a glance", *restock.Color)
	}
}

// A queued event may be delivered minutes after the stock actually moved, and
// the operator is acting on when it moved.
//
// @spec NOTIFY-ALERT-005
func TestEmbedStatesWhenTheChangeWasDetectedNotWhenItWasSent(t *testing.T) {
	e := testEvent(utils.Restock)
	embed := buildEmbed(e)

	if embed.Timestamp == nil {
		t.Fatal("embed carries no timestamp")
	}

	got, err := time.Parse(time.RFC3339, *embed.Timestamp)
	if err != nil {
		t.Fatalf("embed timestamp %q is not RFC 3339: %v", *embed.Timestamp, err)
	}
	if !got.Equal(e.DetectedAt) {
		t.Errorf("embed timestamp = %s, want the detection time %s", got, e.DetectedAt)
	}
}

// @spec NOTIFY-ALERT-006
func TestEmbedShowsTheProductsFirstImage(t *testing.T) {
	embed := buildEmbed(testEvent(utils.Restock))

	if embed.Thumbnail == nil {
		t.Fatal("embed carries no thumbnail for a product that has images")
	}
	if embed.Thumbnail.URL != "https://cdn.test/first.jpg" {
		t.Errorf("thumbnail = %q, want the product's first image", embed.Thumbnail.URL)
	}
}

// @spec NOTIFY-ALERT-007
func TestEmbedWithoutImagesIsStillAnAlert(t *testing.T) {
	e := testEvent(utils.Restock)
	e.Product.Images = nil

	embed := buildEmbed(e)

	if embed.Thumbnail != nil {
		t.Errorf("thumbnail = %+v for a product with no images, want none", embed.Thumbnail)
	}
	if embed.Title == nil || *embed.Title == "" {
		t.Error("an imageless product produced no alert; the image is not what makes it worth sending")
	}
}

// Discord refuses an overlong value outright, and this segment treats that
// refusal as permanent — so an untruncated alert is a lost one.
//
// @spec NOTIFY-DISCORD-004
func TestEmbedShortensValuesDiscordWouldRefuse(t *testing.T) {
	e := testEvent(utils.Restock)
	e.Product.Title = strings.Repeat("A Very Long Product Name ", 40)

	embed := buildEmbed(e)

	if embed.Title == nil {
		t.Fatal("embed carries no title")
	}
	if len([]rune(*embed.Title)) > discordTitleLimit {
		t.Errorf("title is %d characters, over Discord's limit of %d", len([]rune(*embed.Title)), discordTitleLimit)
	}
	for _, f := range embed.Fields {
		if len([]rune(f.Value)) > discordFieldValueLimit {
			t.Errorf("field %q is %d characters, over Discord's limit of %d", f.Name, len([]rune(f.Value)), discordFieldValueLimit)
		}
	}
}
