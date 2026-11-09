package localmatrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// intent is one Matrix user acting in the rooms: a network user's ghost,
// the bridge bot, or the owner.
type intent struct {
	c     *Connector
	mxid  id.UserID
	ghost networkid.UserID
	owner bool
	bot   bool
}

var _ bridgev2.MatrixAPI = (*intent)(nil)

func (in *intent) GetMXID() id.UserID   { return in.mxid }
func (in *intent) IsDoublePuppet() bool { return in.owner }

// SendMessage stores what bridgev2 would send to Matrix. Only events that
// carry a network message, reaction or redaction reach the outbox: the
// bot's notices are bridgev2 talking to a Matrix user who is not there.
func (in *intent) SendMessage(ctx context.Context, roomID id.RoomID, evtType event.Type, content *event.Content, extra *bridgev2.MatrixSendExtra) (*mautrix.RespSendEvent, error) {
	if evtType.IsState() {
		ts := time.Time{}
		if extra != nil {
			ts = extra.Timestamp
		}
		return in.SendState(ctx, roomID, evtType, "", content, ts)
	}
	if in.c.isStopped() {
		return nil, errors.New("the bridge is stopping")
	}
	var evtID id.EventID
	var err error
	switch evtType {
	case event.EventMessage, event.EventSticker, event.EventUnstablePollStart:
		if extra == nil || extra.MessageMeta == nil {
			return &mautrix.RespSendEvent{EventID: newEventID()}, nil
		}
		evtID, err = in.c.handleMessage(ctx, in, roomID, evtType, content, extra)
	case event.EventReaction:
		evtID, err = in.c.handleReaction(ctx, in, roomID, content, extra)
	case event.EventRedaction:
		evtID, err = in.c.handleRedaction(ctx, in, roomID, content, extra)
	default:
		evtID = newEventID()
	}
	if err != nil {
		return nil, err
	}
	return &mautrix.RespSendEvent{EventID: evtID}, nil
}

// SendState applies a state event to the room it names.
func (in *intent) SendState(ctx context.Context, roomID id.RoomID, evtType event.Type, stateKey string, content *event.Content, ts time.Time) (*mautrix.RespSendEvent, error) {
	if in.c.isStopped() {
		return nil, errors.New("the bridge is stopping")
	}
	if err := in.c.handleState(ctx, roomID, evtType, stateKey, content, ts); err != nil {
		return nil, err
	}
	return &mautrix.RespSendEvent{EventID: newEventID()}, nil
}

// MarkRead is a read marker; see Connector.markRead.
func (in *intent) MarkRead(ctx context.Context, roomID id.RoomID, eventID id.EventID, ts time.Time) error {
	return in.c.markRead(ctx, in, roomID, eventID, ts)
}

// MarkUnread is the owner marking a chat unread on another device.
func (in *intent) MarkUnread(ctx context.Context, roomID id.RoomID, unread bool) error {
	if !in.owner {
		return nil
	}
	return in.c.roomFlag(ctx, roomID, store.FlagMarkedUnread, int64(boolToInt(unread)), func(chat *contract.Chat) {
		chat.MarkedUnread = contract.Ptr(unread)
	})
}

// MarkTyping is not mirrored: typing is gone in seconds.
func (in *intent) MarkTyping(context.Context, id.RoomID, bridgev2.TypingType, time.Duration) error {
	return nil
}

func (in *intent) mediaPath(uri id.ContentURIString, file *event.EncryptedFileInfo) (string, error) {
	if file != nil {
		uri = file.URL
	}
	mediaID, ok := store.ParseContentURI(string(uri))
	if !ok {
		return "", fmt.Errorf("unknown media %s", uri)
	}
	return in.c.store.MediaPath(mediaID), nil
}

// DownloadMedia reads a stored file.
func (in *intent) DownloadMedia(_ context.Context, uri id.ContentURIString, file *event.EncryptedFileInfo) ([]byte, error) {
	path, err := in.mediaPath(uri, file)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// DownloadMediaToFile hands a stored file to a callback; a writable one
// gets a copy, so the network may change it without touching the store.
func (in *intent) DownloadMediaToFile(_ context.Context, uri id.ContentURIString, file *event.EncryptedFileInfo, writable bool, callback func(*os.File) error) error {
	path, err := in.mediaPath(uri, file)
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	if !writable {
		return callback(src)
	}
	tmp, err := in.c.store.TempMediaFile()
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, src); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return callback(tmp)
}

// UploadMedia stores a file the network sent.
func (in *intent) UploadMedia(ctx context.Context, _ id.RoomID, data []byte, fileName, mimeType string) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	info, err := in.c.store.PutMedia(ctx, data, mimeType, fileName)
	if err != nil {
		return "", nil, err
	}
	return id.ContentURIString(store.ContentURI(info.ID)), nil, nil
}

// UploadMediaStream stores a file the network streams.
func (in *intent) UploadMediaStream(ctx context.Context, _ id.RoomID, _ int64, _ bool, cb bridgev2.FileStreamCallback) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	tmp, err := in.c.store.TempMediaFile()
	if err != nil {
		return "", nil, err
	}
	res, err := cb(tmp)
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	if closeErr != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, closeErr
	}
	path := tmp.Name()
	if res.ReplacementFile != "" {
		_ = os.Remove(path)
		path = res.ReplacementFile
	}
	info, err := in.c.store.PutMediaFile(ctx, path, res.MimeType, res.FileName)
	if err != nil {
		return "", nil, err
	}
	return id.ContentURIString(store.ContentURI(info.ID)), nil, nil
}

// SetDisplayName is a network user's name changing.
func (in *intent) SetDisplayName(ctx context.Context, name string) error {
	if in.ghost == "" {
		return nil
	}
	return in.c.ghostProfile(ctx, in.ghost, func(g *store.Ghost) { g.Name = name })
}

// SetAvatarURL is a network user's picture changing.
func (in *intent) SetAvatarURL(ctx context.Context, avatarURL id.ContentURIString) error {
	if in.ghost == "" {
		return nil
	}
	return in.c.ghostProfile(ctx, in.ghost, func(g *store.Ghost) { g.Avatar = string(avatarURL) })
}

// profileExtra is the part of bridgev2's extra profile the contract reads.
type profileExtra struct {
	Identifiers []string `json:"com.beeper.bridge.identifiers"`
	IsBot       bool     `json:"com.beeper.bridge.is_network_bot"`
}

func readProfileExtra(data any) (profileExtra, bool) {
	var extra profileExtra
	raw, err := json.Marshal(data)
	if err != nil {
		return extra, false
	}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return extra, false
	}
	return extra, true
}

// SetExtraProfileMeta is a network user's numbers, addresses or bot flag
// changing.
func (in *intent) SetExtraProfileMeta(ctx context.Context, data any) error {
	if in.ghost == "" {
		return nil
	}
	extra, ok := readProfileExtra(data)
	if !ok {
		return nil
	}
	return in.c.ghostProfile(ctx, in.ghost, func(g *store.Ghost) {
		g.Identifiers = extra.Identifiers
		g.IsBot = extra.IsBot
	})
}

// SetProfile is a whole profile at once.
func (in *intent) SetProfile(ctx context.Context, data any) error {
	if in.ghost == "" {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var whole struct {
		Name   string `json:"displayname"`
		Avatar string `json:"avatar_url"`
	}
	_ = json.NewDecoder(bytes.NewReader(raw)).Decode(&whole)
	extra, _ := readProfileExtra(data)
	return in.c.ghostProfile(ctx, in.ghost, func(g *store.Ghost) {
		g.Name, g.Avatar = whole.Name, whole.Avatar
		g.Identifiers, g.IsBot = extra.Identifiers, extra.IsBot
	})
}

// CreateRoom is a new portal.
func (in *intent) CreateRoom(ctx context.Context, req *mautrix.ReqCreateRoom) (id.RoomID, error) {
	return in.c.createRoom(ctx, req)
}

// DeleteRoom is a chat gone on the network.
func (in *intent) DeleteRoom(ctx context.Context, roomID id.RoomID, puppetsOnly bool) error {
	if puppetsOnly {
		return nil
	}
	return in.c.deleteRoom(ctx, roomID)
}

// EnsureJoined puts a user in a room.
func (in *intent) EnsureJoined(ctx context.Context, roomID id.RoomID, _ ...bridgev2.EnsureJoinedParams) error {
	if in.bot {
		return nil
	}
	content := &event.Content{Parsed: &event.MemberEventContent{Membership: event.MembershipJoin}}
	return in.c.handleState(ctx, roomID, event.StateMember, string(in.mxid), content, time.Time{})
}

// EnsureInvited puts a user in a room; invites are joined at once.
func (in *intent) EnsureInvited(ctx context.Context, roomID id.RoomID, userID id.UserID) error {
	if userID == in.c.BotMXID() {
		return nil
	}
	content := &event.Content{Parsed: &event.MemberEventContent{Membership: event.MembershipJoin}}
	return in.c.handleState(ctx, roomID, event.StateMember, string(userID), content, time.Time{})
}

// TagRoom is the owner pinning or archiving a chat on another device: the
// favourite tag is pinned, low priority is archived.
func (in *intent) TagRoom(ctx context.Context, roomID id.RoomID, tag event.RoomTag, isTagged bool) error {
	if !in.owner {
		return nil
	}
	switch tag {
	case event.RoomTagFavourite:
		return in.c.roomFlag(ctx, roomID, store.FlagPinned, int64(boolToInt(isTagged)), func(chat *contract.Chat) {
			chat.Pinned = contract.Ptr(isTagged)
		})
	case event.RoomTagLowPriority:
		return in.c.roomFlag(ctx, roomID, store.FlagArchived, int64(boolToInt(isTagged)), func(chat *contract.Chat) {
			chat.Archived = contract.Ptr(isTagged)
		})
	}
	return nil
}

// MuteRoom is the owner muting a chat on another device.
func (in *intent) MuteRoom(ctx context.Context, roomID id.RoomID, until time.Time) error {
	if !in.owner {
		return nil
	}
	var ms int64
	if until.After(in.c.store.Now()) {
		ms = until.UnixMilli()
	}
	return in.c.roomFlag(ctx, roomID, store.FlagMutedUntil, ms, func(chat *contract.Chat) {
		if ms == 0 {
			chat.MutedUntil = contract.Clear[contract.Instant]()
		} else {
			chat.MutedUntil = contract.Set(contract.At(until))
		}
	})
}

// GetEvent is unknown for every event: the store keeps folds, not Matrix
// events, and bridgev2 treats not found as a homeserver would.
func (in *intent) GetEvent(context.Context, id.RoomID, id.EventID) (*event.Event, error) {
	return nil, mautrix.MNotFound
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
