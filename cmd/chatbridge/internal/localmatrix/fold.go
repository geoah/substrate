package localmatrix

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// partFold is one message part reduced to what the contract carries. It is
// stored per part, so an edit to one part refolds the message from the rest.
type partFold struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text,omitempty"`
	Caption          string   `json:"caption,omitempty"`
	Media            string   `json:"media,omitempty"`
	MediaState       string   `json:"mediaState,omitempty"`
	MimeType         string   `json:"mimeType,omitempty"`
	FileName         string   `json:"fileName,omitempty"`
	Size             int64    `json:"size,omitempty"`
	Width            int      `json:"width,omitempty"`
	Height           int      `json:"height,omitempty"`
	DurationMs       int64    `json:"durationMs,omitempty"`
	Mentions         []string `json:"mentions,omitempty"`
	MentionsEveryone bool     `json:"mentionsEveryone,omitempty"`
	ReplyTo          string   `json:"replyTo,omitempty"`
	ThreadRoot       string   `json:"threadRoot,omitempty"`
	ExpiresInMs      int64    `json:"expiresInMs,omitempty"`
}

// isMedia reports a part that is a file rather than text.
func (p partFold) isMedia() bool {
	switch p.Kind {
	case contract.ContentImage, contract.ContentVideo, contract.ContentAudio, contract.ContentVoice,
		contract.ContentFile, contract.ContentSticker:
		return true
	}
	return false
}

// mediaChooser decides whether a file's bytes ride along, from the owner's
// policy for the account and what the store holds.
type mediaChooser func(kind, mediaID string, size int64) (state string, ship bool)

// foldContent reduces one Matrix message part. parseGhost maps a mentioned
// Matrix user to the network's user id.
func foldContent(evtType event.Type, content *event.Content, parseGhost func(string) (string, bool), choose mediaChooser) partFold {
	if content.Parsed == nil && content.VeryRaw != nil {
		_ = content.ParseRaw(evtType)
	}
	var f partFold
	switch c := content.Parsed.(type) {
	case *event.MessageEventContent:
		f = foldMessageContent(evtType, c, choose)
		if c.Mentions != nil {
			for _, u := range c.Mentions.UserIDs {
				if netID, ok := parseGhost(string(u)); ok && !slices.Contains(f.Mentions, netID) {
					f.Mentions = append(f.Mentions, netID)
				}
			}
			f.MentionsEveryone = c.Mentions.Room
		}
		if c.BeeperDisappearingTimer != nil && c.BeeperDisappearingTimer.Type == event.DisappearingTypeAfterSend {
			f.ExpiresInMs = c.BeeperDisappearingTimer.Timer.Milliseconds()
		}
	case *event.PollStartEventContent:
		f.Kind = contract.ContentPoll
		var b strings.Builder
		b.WriteString(c.PollStart.Question.Text)
		for _, a := range c.PollStart.Answers {
			b.WriteString("\n- ")
			b.WriteString(a.Text)
		}
		f.Text = b.String()
	default:
		f.Kind = contract.ContentUnsupported
	}
	return f
}

func foldMessageContent(evtType event.Type, c *event.MessageEventContent, choose mediaChooser) partFold {
	var f partFold
	switch {
	case evtType == event.EventSticker:
		f.Kind = contract.ContentSticker
	case c.MsgType == event.MsgText:
		f.Kind = contract.ContentText
	case c.MsgType == event.MsgNotice:
		f.Kind = contract.ContentNotice
	case c.MsgType == event.MsgEmote:
		f.Kind = contract.ContentEmote
	case c.MsgType == event.MsgImage:
		f.Kind = contract.ContentImage
	case c.MsgType == event.MsgVideo:
		f.Kind = contract.ContentVideo
	case c.MsgType == event.MsgAudio && c.MSC3245Voice != nil:
		f.Kind = contract.ContentVoice
	case c.MsgType == event.MsgAudio:
		f.Kind = contract.ContentAudio
	case c.MsgType == event.MsgFile:
		f.Kind = contract.ContentFile
	case c.MsgType == event.MsgLocation:
		f.Kind = contract.ContentLocation
	default:
		f.Kind = contract.ContentUnsupported
	}
	if !f.isMedia() {
		f.Text = messageText(c)
		if f.Kind == contract.ContentLocation && c.GeoURI != "" && !strings.Contains(f.Text, c.GeoURI) {
			f.Text = strings.TrimSpace(f.Text + "\n" + c.GeoURI)
		}
		return f
	}
	// A file's body is its caption when a filename sits beside it, else the
	// filename itself.
	f.FileName = c.FileName
	if f.FileName == "" {
		f.FileName = c.Body
	} else if c.Body != "" && c.Body != c.FileName {
		f.Caption = messageText(c)
	}
	if c.Info != nil {
		f.MimeType = c.Info.MimeType
		f.Size = int64(c.Info.Size)
		f.Width, f.Height = c.Info.Width, c.Info.Height
		f.DurationMs = int64(c.Info.Duration)
	}
	uri := string(c.URL)
	if c.File != nil {
		uri = string(c.File.URL)
	}
	mediaID, ok := store.ParseContentURI(uri)
	switch {
	case choose == nil:
		f.MediaState = contract.MediaMetadata
	case !ok:
		f.MediaState = contract.MediaFailed
	default:
		state, ship := choose(f.Kind, mediaID, f.Size)
		f.MediaState = state
		if ship {
			f.Media = mediaID
		}
	}
	return f
}

// messageText is a message's text as markdown, from its HTML when it has
// any.
func messageText(c *event.MessageEventContent) string {
	if c.Format == event.FormatHTML && c.FormattedBody != "" {
		return strings.TrimSpace(format.HTMLToMarkdown(c.FormattedBody))
	}
	return c.Body
}

// foldedPart is a stored part with its fold read back.
type foldedPart struct {
	store.Part
	fold partFold
}

func readParts(parts []store.Part) []foldedPart {
	out := make([]foldedPart, 0, len(parts))
	for _, p := range parts {
		var f partFold
		_ = json.Unmarshal([]byte(p.Content), &f)
		out = append(out, foldedPart{Part: p, fold: f})
	}
	return out
}

// foldMessage folds a message's parts into one message: the text parts'
// texts joined with a blank line, the files as attachments keyed by part.
// mediaKnown reports whether a file's bytes can still be sent; when they
// cannot, the attachment leaves its file out, which keeps what the server
// holds.
func foldMessage(parts []foldedPart, mediaKnown func(string) bool) contract.Message {
	first := parts[0]
	msg := contract.Message{
		ID:        first.MessageID,
		Sender:    first.Sender,
		FromOwner: first.FromOwner,
		At:        contract.At(time.UnixMilli(first.TS)),
	}
	var texts []string
	var editCount int
	var editedAt int64
	var mediaKind string
	for _, p := range parts {
		f := p.fold
		if p.EditCount > editCount {
			editCount = p.EditCount
		}
		if p.EditedAt != nil && *p.EditedAt > editedAt {
			editedAt = *p.EditedAt
		}
		if p.Action != "" && msg.Action == nil {
			msg.Action = contract.Ptr(p.Action)
		}
		if f.ReplyTo != "" && msg.ReplyTo == nil {
			msg.ReplyTo = contract.Ptr(f.ReplyTo)
		}
		if f.ThreadRoot != "" && msg.ThreadRoot == nil {
			msg.ThreadRoot = contract.Ptr(f.ThreadRoot)
		}
		for _, m := range f.Mentions {
			if !slices.Contains(msg.Mentions, m) {
				msg.Mentions = append(msg.Mentions, m)
			}
		}
		if f.MentionsEveryone {
			msg.MentionsEveryone = contract.Ptr(true)
		}
		if f.ExpiresInMs > 0 && msg.ExpiresAt == nil {
			msg.ExpiresAt = contract.AtPtr(time.UnixMilli(first.TS + f.ExpiresInMs))
		}
		if !f.isMedia() {
			if f.Text != "" {
				texts = append(texts, f.Text)
			}
			continue
		}
		if mediaKind == "" {
			mediaKind = f.Kind
		}
		att := contract.Attachment{
			Part:       p.PartID,
			Content:    f.Kind,
			MediaState: f.MediaState,
		}
		if f.MimeType != "" {
			att.MimeType = contract.Ptr(f.MimeType)
		}
		if f.FileName != "" {
			att.FileName = contract.Ptr(f.FileName)
		}
		if f.Size > 0 {
			att.Size = contract.Ptr(f.Size)
		}
		if f.Width > 0 {
			att.Width = contract.Ptr(f.Width)
		}
		if f.Height > 0 {
			att.Height = contract.Ptr(f.Height)
		}
		if f.DurationMs > 0 {
			att.DurationMs = contract.Ptr(f.DurationMs)
		}
		if f.Caption != "" {
			att.Caption = contract.Ptr(f.Caption)
		}
		if f.Media != "" && mediaKnown(f.Media) {
			att.File = &contract.Media{Part: f.Media}
		}
		msg.Attachments = append(msg.Attachments, att)
	}
	switch {
	case mediaKind != "":
		msg.Content = mediaKind
	default:
		msg.Content = first.fold.Kind
	}
	if len(texts) > 0 {
		msg.Text = contract.Ptr(strings.Join(texts, "\n\n"))
	}
	if editCount > 0 {
		msg.EditCount = contract.Ptr(editCount)
		if editedAt > 0 {
			msg.EditedAt = contract.AtPtr(time.UnixMilli(editedAt))
		}
	}
	return msg
}

// messageMedia are the files a folded message sends.
func messageMedia(msg contract.Message) []string {
	var out []string
	for _, a := range msg.Attachments {
		if a.File != nil {
			out = append(out, a.File.Part)
		}
	}
	return out
}

// reactionKey normalizes a reaction for comparison: bridgev2 adds the
// emoji variation selector for Matrix, which the network never sent.
func reactionKey(key string) string { return variationselector.Remove(key) }

// features maps bridgev2's room features to the contract's.
func features(rf *event.RoomFeatures) *contract.Features {
	if rf == nil {
		return nil
	}
	f := &contract.Features{
		Edit:              rf.Edit.Partial(),
		DeleteForEveryone: rf.Delete.Partial(),
		Reactions:         rf.Reaction.Partial(),
		Replies:           rf.Reply.Partial(),
		Threads:           rf.Thread.Partial(),
		ReadReceipts:      rf.ReadReceipts,
		Typing:            rf.TypingNotifications,
	}
	if rf.EditMaxAge != nil && rf.EditMaxAge.Duration > 0 {
		f.EditMaxAgeSeconds = contract.Ptr(int64(rf.EditMaxAge.Seconds()))
	}
	if rf.DeleteMaxAge != nil && rf.DeleteMaxAge.Duration > 0 {
		f.DeleteMaxAgeSeconds = contract.Ptr(int64(rf.DeleteMaxAge.Seconds()))
	}
	if len(rf.AllowedReactions) > 0 {
		f.AllowedReactions = slices.Clone(rf.AllowedReactions)
	}
	if rf.MaxTextLength > 0 {
		f.MaxTextLength = contract.Ptr(rf.MaxTextLength)
	}
	return f
}

// role maps a Matrix power level to a member's role.
func role(power int) string {
	switch {
	case power >= 95:
		return contract.RoleOwner
	case power >= 50:
		return contract.RoleAdmin
	default:
		return contract.RoleMember
	}
}

// membership maps a Matrix membership to the contract's.
func membership(m event.Membership) string {
	switch m {
	case event.MembershipJoin:
		return contract.MembershipJoin
	case event.MembershipInvite:
		return contract.MembershipInvite
	case event.MembershipBan:
		return contract.MembershipBan
	default:
		return contract.MembershipLeave
	}
}
