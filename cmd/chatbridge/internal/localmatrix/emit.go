package localmatrix

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// roomScope is what a room is on the network: whose login and which chat.
type roomScope struct {
	roomID  id.RoomID
	key     networkid.PortalKey
	account string
	chat    string
	login   *bridgev2.UserLogin
}

// scope resolves a room. A room the side cannot place, or one shared by
// every login (portals are split, so none should be), is skipped: its
// events would name no account.
func (c *Connector) scope(ctx context.Context, tx *store.Tx, roomID id.RoomID) (*roomScope, error) {
	key, ok, err := c.portalKey(ctx, tx, roomID)
	if err != nil || !ok || key.Receiver == "" {
		return nil, err
	}
	sc := &roomScope{roomID: roomID, key: key, account: string(key.Receiver), chat: string(key.ID)}
	if c.br != nil {
		sc.login = c.br.GetCachedUserLoginByID(key.Receiver)
	}
	return sc, nil
}

func (c *Connector) head(account, typ string, at time.Time) contract.Head {
	h := contract.Head{Type: typ, Network: c.network, Account: account}
	if !at.IsZero() {
		h.At = contract.At(at)
	}
	return h
}

// isSelf reports whether a network user is the login's own.
func (c *Connector) isSelf(ctx context.Context, sc *roomScope, user networkid.UserID) bool {
	if sc == nil || sc.login == nil || user == "" {
		return false
	}
	if c.net != nil && c.net.SelfUserID(sc.login) == user {
		return true
	}
	return sc.login.Client != nil && sc.login.Client.IsThisUser(ctx, user)
}

// selfUser is the login's own user id.
func (c *Connector) selfUser(sc *roomScope) string {
	if sc == nil || sc.login == nil || c.net == nil {
		return ""
	}
	return string(c.net.SelfUserID(sc.login))
}

// userOf maps a Matrix user in a room to the network's user id: a ghost to
// its id, the owner to the login's own id, the bot to none.
func (c *Connector) userOf(sc *roomScope, mxid string) (string, bool) {
	if mxid == string(OwnerMXID) {
		self := c.selfUser(sc)
		return self, self != ""
	}
	u, ok := c.ParseGhostMXID(id.UserID(mxid))
	return string(u), ok
}

// chooser decides which files ride along, from the account's policy.
func (c *Connector) chooser(ctx context.Context, tx *store.Tx, account string) mediaChooser {
	policy := MediaSmall
	if c.settings != nil {
		policy = c.settings.MediaPolicy(c.network, account)
	}
	return func(kind, mediaID string, size int64) (string, bool) {
		info, err := tx.Media(ctx, mediaID)
		if err != nil || info == nil {
			return contract.MediaFailed, false
		}
		if size <= 0 {
			size = info.Size
		}
		if size > contract.MaxMediaPart {
			return contract.MediaTooLarge, false
		}
		switch policy {
		case MediaNone:
			return contract.MediaMetadata, false
		case MediaAll:
			return contract.MediaStored, true
		default:
			switch kind {
			case contract.ContentImage, contract.ContentVoice, contract.ContentSticker:
				return contract.MediaStored, true
			}
			return contract.MediaMetadata, false
		}
	}
}

func (c *Connector) mediaKnown(ctx context.Context, tx *store.Tx) func(string) bool {
	return func(mediaID string) bool {
		info, err := tx.Media(ctx, mediaID)
		return err == nil && info != nil
	}
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// ensureUser sends a user to an account before an event names them, and
// again whenever their profile changed since.
func (c *Connector) ensureUser(ctx context.Context, tx *store.Tx, sc *roomScope, userID string) error {
	if userID == "" || sc == nil {
		return nil
	}
	u, media, err := c.buildUser(ctx, tx, sc, userID)
	if err != nil {
		return err
	}
	d := digest(u)
	old, seen, err := tx.UserDigest(ctx, c.network, sc.account, userID)
	if err != nil || (seen && old == d) {
		return err
	}
	ev := &contract.UserUpsert{Head: c.head(sc.account, contract.TypeUserUpsert, time.Time{}), User: u}
	if _, err := tx.Append(ctx, ev, media...); err != nil {
		return err
	}
	return tx.SetUserDigest(ctx, c.network, sc.account, userID, d)
}

func (c *Connector) buildUser(ctx context.Context, tx *store.Tx, sc *roomScope, userID string) (contract.User, []string, error) {
	u := contract.User{ID: userID}
	g, err := tx.Ghost(ctx, c.network, userID)
	if err != nil {
		return u, nil, err
	}
	if g == nil && c.br != nil {
		// A ghost bridgev2 knew before this side did (a restart, an old
		// store): read its row once.
		row, err := c.br.DB.Ghost.GetByID(ctx, networkid.UserID(userID))
		if err == nil && row != nil {
			g = &store.Ghost{
				Network: c.network, UserID: userID, Name: row.Name, Avatar: string(row.AvatarMXC),
				Identifiers: row.Identifiers, IsBot: row.IsBot,
			}
			if err := tx.PutGhost(ctx, *g); err != nil {
				return u, nil, err
			}
		}
	}
	var media []string
	if g != nil {
		if g.Name != "" {
			u.FullName = contract.Ptr(g.Name)
		}
		for _, ident := range g.Identifiers {
			switch {
			case strings.HasPrefix(ident, "tel:"):
				u.PhoneNumbers = append(u.PhoneNumbers, strings.TrimPrefix(ident, "tel:"))
			case strings.HasPrefix(ident, "mailto:"):
				u.Emails = append(u.Emails, strings.TrimPrefix(ident, "mailto:"))
			case strings.HasPrefix(ident, c.network+":") && u.Handle == nil:
				// A network's own identifier is its user name there
				// (telegram:ada).
				u.Handle = contract.Ptr(strings.TrimPrefix(ident, c.network+":"))
			}
		}
		u.IsBot = contract.Ptr(g.IsBot)
		avatar, ship := c.avatar(ctx, tx, sc.account, g.Avatar)
		u.Avatar = avatar
		if ship != "" {
			media = append(media, ship)
		}
	}
	if sc.login != nil {
		u.IsSelf = contract.Ptr(c.isSelf(ctx, sc, networkid.UserID(userID)))
	}
	if c.net != nil && sc.login != nil {
		x := c.net.UserExtras(ctx, sc.login, networkid.UserID(userID))
		if x.Known {
			u.IsContact = contract.Ptr(x.IsContact)
		}
		if x.NameKnown {
			u.ContactName = contract.SetOrClear(x.ContactName)
		}
		if x.Handle != "" {
			u.Handle = contract.Ptr(x.Handle)
		}
	}
	return u, media, nil
}

// avatar decides an avatar field: cleared when there is none, the file when
// the policy ships it, absent otherwise.
func (c *Connector) avatar(ctx context.Context, tx *store.Tx, account, uri string) (contract.Null[contract.Media], string) {
	if uri == "" {
		return contract.Clear[contract.Media](), ""
	}
	mediaID, ok := store.ParseContentURI(uri)
	if !ok {
		return contract.Null[contract.Media]{}, ""
	}
	state, ship := c.chooser(ctx, tx, account)(contract.ContentImage, mediaID, 0)
	if !ship || state != contract.MediaStored {
		return contract.Null[contract.Media]{}, ""
	}
	return contract.Set(contract.Media{Part: mediaID}), mediaID
}

// emitMessage folds a message from its stored parts and appends it, after
// the users it names.
func (c *Connector) emitMessage(ctx context.Context, tx *store.Tx, sc *roomScope, messageID string, backfill bool) error {
	if err := c.ensureChat(ctx, tx, sc); err != nil {
		return err
	}
	parts, err := tx.MessageParts(ctx, string(sc.roomID), messageID)
	if err != nil || len(parts) == 0 {
		return err
	}
	for _, p := range parts {
		if p.Deleted == contract.ScopeEveryone {
			// A deleted message stays deleted against a later copy.
			return nil
		}
	}
	msg := foldMessage(readParts(parts), c.mediaKnown(ctx, tx))
	if err := c.ensureUser(ctx, tx, sc, msg.Sender); err != nil {
		return err
	}
	for _, m := range msg.Mentions {
		if err := c.ensureUser(ctx, tx, sc, m); err != nil {
			return err
		}
	}
	ev := &contract.MessageUpsert{
		Head:     c.head(sc.account, contract.TypeMessageUpsert, time.Time{}),
		Chat:     sc.chat,
		Backfill: backfill,
		Message:  msg,
	}
	_, err = tx.Append(ctx, ev, messageMedia(msg)...)
	return err
}

// emitReactions appends the whole set of reactions one message holds.
func (c *Connector) emitReactions(ctx context.Context, tx *store.Tx, sc *roomScope, messageID string) error {
	if err := c.ensureChat(ctx, tx, sc); err != nil {
		return err
	}
	rows, err := tx.MessageReactions(ctx, string(sc.roomID), messageID)
	if err != nil {
		return err
	}
	reactions := make([]contract.Reaction, 0, len(rows))
	for _, r := range rows {
		if err := c.ensureUser(ctx, tx, sc, r.Sender); err != nil {
			return err
		}
		reactions = append(reactions, contract.Reaction{Key: r.Key, User: r.Sender, At: contract.At(time.UnixMilli(r.TS))})
	}
	ev := &contract.MessageReactions{
		Head:      c.head(sc.account, contract.TypeMessageReactions, time.Time{}),
		Chat:      sc.chat,
		Message:   messageID,
		Reactions: reactions,
	}
	_, err = tx.Append(ctx, ev)
	return err
}

func eventTime(extra *bridgev2.MatrixSendExtra, fallback time.Time) time.Time {
	if extra != nil && !extra.Timestamp.IsZero() {
		return extra.Timestamp
	}
	if extra != nil && extra.MessageMeta != nil && !extra.MessageMeta.Timestamp.IsZero() {
		return extra.MessageMeta.Timestamp
	}
	return fallback
}

func parseContent(evtType event.Type, content *event.Content) {
	if content.Parsed != nil {
		return
	}
	if content.VeryRaw == nil && content.Raw != nil {
		content.VeryRaw, _ = json.Marshal(content.Raw)
	}
	if content.VeryRaw != nil {
		_ = content.ParseRaw(evtType)
	}
}

// handleMessage stores one message part, or an edit of one, and appends
// the message as it now stands.
func (c *Connector) handleMessage(ctx context.Context, in *intent, roomID id.RoomID, evtType event.Type, content *event.Content, extra *bridgev2.MatrixSendExtra) (id.EventID, error) {
	meta := extra.MessageMeta
	evtID := c.GenerateDeterministicEventID(roomID, meta.Room, meta.ID, meta.PartID)
	parseContent(evtType, content)
	ts := eventTime(extra, c.store.Now())
	msgContent, _ := content.Parsed.(*event.MessageEventContent)
	isEdit := msgContent != nil && msgContent.RelatesTo != nil && msgContent.RelatesTo.Type == event.RelReplace
	foldFrom := content
	if isEdit && msgContent.NewContent != nil {
		foldFrom = &event.Content{Parsed: msgContent.NewContent}
	}
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		f := foldContent(evtType, foldFrom, func(m string) (string, bool) { return c.userOf(sc, m) }, c.chooser(ctx, tx, sc.account))
		f.ReplyTo = string(meta.ReplyTo.MessageID)
		f.ThreadRoot = string(meta.ThreadRoot)
		if isEdit {
			edited, err := c.editPart(ctx, tx, sc, meta, f, ts)
			if err != nil || edited {
				return err
			}
		}
		raw, err := json.Marshal(f)
		if err != nil {
			return err
		}
		part := store.Part{
			EventID:   string(evtID),
			RoomID:    string(roomID),
			MessageID: string(meta.ID),
			PartID:    string(meta.PartID),
			Index:     extra.PartIndex,
			Sender:    string(meta.SenderID),
			FromOwner: in.owner || c.isSelf(ctx, sc, meta.SenderID),
			TS:        ts.UnixMilli(),
			Content:   string(raw),
		}
		if isEdit {
			part.EditCount = 1
			part.EditedAt = contract.Ptr(ts.UnixMilli())
		}
		if err := tx.PutPart(ctx, part); err != nil {
			return err
		}
		return c.emitMessage(ctx, tx, sc, string(meta.ID), false)
	})
	return evtID, err
}

// editPart applies an edit to a part the store holds, keeping what the
// edit does not carry (relations, expiry), and reports whether it did.
func (c *Connector) editPart(ctx context.Context, tx *store.Tx, sc *roomScope, meta *database.Message, f partFold, ts time.Time) (bool, error) {
	parts, err := tx.MessageParts(ctx, string(sc.roomID), string(meta.ID))
	if err != nil {
		return false, err
	}
	for _, p := range readParts(parts) {
		if p.PartID != string(meta.PartID) {
			continue
		}
		if f.ReplyTo == "" {
			f.ReplyTo = p.fold.ReplyTo
		}
		if f.ThreadRoot == "" {
			f.ThreadRoot = p.fold.ThreadRoot
		}
		if f.ExpiresInMs == 0 {
			f.ExpiresInMs = p.fold.ExpiresInMs
		}
		raw, err := json.Marshal(f)
		if err != nil {
			return false, err
		}
		if _, err := tx.EditPart(ctx, string(sc.roomID), p.MessageID, p.PartID, string(raw), ts.UnixMilli()); err != nil {
			return false, err
		}
		return true, c.emitMessage(ctx, tx, sc, p.MessageID, false)
	}
	return false, nil
}

// handleReaction stores one reaction and appends the message's whole set.
func (c *Connector) handleReaction(ctx context.Context, in *intent, roomID id.RoomID, content *event.Content, extra *bridgev2.MatrixSendExtra) (id.EventID, error) {
	parseContent(event.EventReaction, content)
	rc, ok := content.Parsed.(*event.ReactionEventContent)
	if !ok {
		return newEventID(), nil
	}
	var evtID id.EventID
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		var msgID, partID, sender, emojiID string
		var meta *database.Reaction
		if extra != nil {
			meta = extra.ReactionMeta
		}
		if meta != nil {
			msgID, partID, sender, emojiID = string(meta.MessageID), string(meta.MessagePartID), string(meta.SenderID), string(meta.EmojiID)
		} else {
			target, err := tx.PartByEvent(ctx, string(rc.RelatesTo.EventID))
			if err != nil || target == nil {
				return err
			}
			msgID, partID = target.MessageID, target.PartID
			sender = string(in.ghost)
			if in.owner {
				sender = c.selfUser(sc)
			}
		}
		evtID = c.GenerateReactionEventID(roomID, &database.Message{ID: networkid.MessageID(msgID), PartID: networkid.PartID(partID)},
			networkid.UserID(sender), networkid.EmojiID(emojiID))
		if err := tx.PutReaction(ctx, store.ReactionRow{
			EventID: string(evtID), RoomID: string(roomID), MessageID: msgID, Sender: sender, EmojiID: emojiID,
			Key: reactionKey(rc.RelatesTo.Key), TS: eventTime(extra, c.store.Now()).UnixMilli(),
		}); err != nil {
			return err
		}
		return c.emitReactions(ctx, tx, sc, msgID)
	})
	if evtID == "" {
		evtID = newEventID()
	}
	return evtID, err
}

// handleRedaction removes a reaction, deletes a message, or drops a part an
// edit removed.
func (c *Connector) handleRedaction(ctx context.Context, in *intent, roomID id.RoomID, content *event.Content, extra *bridgev2.MatrixSendExtra) (id.EventID, error) {
	parseContent(event.EventRedaction, content)
	rc, ok := content.Parsed.(*event.RedactionEventContent)
	if !ok {
		return newEventID(), nil
	}
	remote := bridgev2.GetRemoteEventFromContext(ctx)
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		if r, err := tx.ReactionByEvent(ctx, string(rc.Redacts)); err != nil {
			return err
		} else if r != nil {
			if err := tx.DeleteReaction(ctx, r.EventID); err != nil {
				return err
			}
			return c.emitReactions(ctx, tx, sc, r.MessageID)
		}
		part, err := tx.PartByEvent(ctx, string(rc.Redacts))
		if err != nil || part == nil {
			return err
		}
		if remote != nil && remote.GetType() == bridgev2.RemoteEventEdit {
			if err := tx.DeletePart(ctx, part.EventID); err != nil {
				return err
			}
			return c.emitMessage(ctx, tx, sc, part.MessageID, false)
		}
		scope := contract.ScopeEveryone
		if only, ok := remote.(bridgev2.RemoteDeleteOnlyForMe); ok && only.DeleteOnlyForMe() {
			scope = contract.ScopeOwner
		}
		changed, err := tx.MarkMessageDeleted(ctx, string(roomID), part.MessageID, scope)
		if err != nil || !changed {
			return err
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		by := string(in.ghost)
		if in.owner {
			by = c.selfUser(sc)
		}
		if remote != nil {
			if s := remote.GetSender(); s.IsFromMe {
				by = c.selfUser(sc)
			} else if s.Sender != "" {
				by = string(s.Sender)
			}
		}
		if err := c.ensureUser(ctx, tx, sc, by); err != nil {
			return err
		}
		ev := &contract.MessageDelete{
			Head:    c.head(sc.account, contract.TypeMessageDelete, eventTime(extra, c.store.Now())),
			Chat:    sc.chat,
			Message: part.MessageID,
			Scope:   scope,
		}
		if by != "" {
			ev.By = contract.Ptr(by)
		}
		_, err = tx.Append(ctx, ev)
		return err
	})
	return newEventID(), err
}

// handleState applies one state event to a chat and appends what changed.
func (c *Connector) handleState(ctx context.Context, roomID id.RoomID, evtType event.Type, stateKey string, content *event.Content, ts time.Time) error {
	evtType.Class = event.StateEventType
	parseContent(evtType, content)
	return c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		chat := contract.Chat{ID: sc.chat}
		var media, users []string
		switch v := content.Parsed.(type) {
		case *event.RoomNameEventContent:
			chat.ChatName = contract.SetOrClear(v.Name)
		case *event.TopicEventContent:
			chat.Topic = contract.SetOrClear(v.Topic)
		case *event.RoomAvatarEventContent:
			avatar, ship := c.avatar(ctx, tx, sc.account, string(v.URL))
			if avatar.IsZero() {
				return nil
			}
			chat.Avatar = avatar
			if ship != "" {
				media = append(media, ship)
			}
		case *event.MemberEventContent:
			user, ok := c.userOf(sc, stateKey)
			if !ok {
				return nil
			}
			m := membership(v.Membership)
			changed, err := tx.SetMember(ctx, string(roomID), stateKey, user, m)
			if err != nil || !changed {
				return err
			}
			users = append(users, user)
			chat.Members = &contract.Members{Items: []contract.Member{{User: user, Membership: m}}}
		case *event.PowerLevelsEventContent:
			items, err := c.powerChanges(ctx, tx, sc, v)
			if err != nil || len(items) == 0 {
				return err
			}
			for _, it := range items {
				users = append(users, it.User)
			}
			chat.Members = &contract.Members{Items: items}
		case *event.RoomFeatures:
			chat.Features = features(v)
		case *event.BeeperDisappearingTimer:
			if v.Type == event.DisappearingTypeAfterSend && v.Timer.Duration > 0 {
				chat.DisappearAfterSeconds = contract.Set(int64(v.Timer.Seconds()))
			} else {
				chat.DisappearAfterSeconds = contract.Clear[int64]()
			}
		case *event.SpaceParentEventContent:
			parent, ok, err := c.portalKey(ctx, tx, id.RoomID(stateKey))
			if err != nil || !ok {
				return err
			}
			chat.Parent = contract.Set(string(parent.ID))
		default:
			return nil
		}
		for _, u := range users {
			if err := c.ensureUser(ctx, tx, sc, u); err != nil {
				return err
			}
		}
		_, err = tx.Append(ctx, &contract.ChatUpsert{Head: c.head(sc.account, contract.TypeChatUpsert, time.Time{}), Chat: chat}, media...)
		return err
	})
}

// powerChanges records each member's power level and returns the members
// whose role changed.
func (c *Connector) powerChanges(ctx context.Context, tx *store.Tx, sc *roomScope, pl *event.PowerLevelsEventContent) ([]contract.Member, error) {
	members, err := tx.Members(ctx, string(sc.roomID))
	if err != nil {
		return nil, err
	}
	var items []contract.Member
	for _, m := range members {
		power := pl.UsersDefault
		if p, ok := pl.Users[id.UserID(m.MXID)]; ok {
			power = p
		}
		changed, err := tx.SetPower(ctx, string(sc.roomID), m.MXID, power)
		if err != nil {
			return nil, err
		}
		if !changed || role(power) == role(m.Power) || m.UserID == "" {
			continue
		}
		items = append(items, contract.Member{User: m.UserID, Membership: m.Membership, Role: contract.Ptr(role(power))})
	}
	return items, nil
}

// maxMembersPerEvent keeps a large group's member list inside one event's
// share of a batch.
const maxMembersPerEvent = 1000

// createRoom records a new portal and appends it whole: its members, flags
// and features.
func (c *Connector) createRoom(ctx context.Context, req *mautrix.ReqCreateRoom) (id.RoomID, error) {
	roomID := req.BeeperLocalRoomID
	if roomID == "" {
		roomID = id.RoomID("!" + hashID("room", store.RandomID(8)) + ":" + ServerName)
	}
	c.roomsMu.Lock()
	key, known := c.rooms[roomID]
	c.roomsMu.Unlock()
	if !known || key.Receiver == "" {
		return roomID, nil
	}
	var portal *bridgev2.Portal
	if c.br != nil {
		portal, _ = c.br.GetExistingPortalByKey(ctx, key)
	}
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		if err := tx.PutRoom(ctx, store.Room{RoomID: string(roomID), Network: c.network, PortalID: string(key.ID), Receiver: string(key.Receiver)}); err != nil {
			return err
		}
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		chat := contract.Chat{ID: sc.chat, ChatName: contract.SetOrClear(req.Name), Topic: contract.SetOrClear(req.Topic)}
		category := contract.CategoryGroup
		if req.IsDirect {
			category = contract.CategoryDirect
		}
		var media []string
		if portal != nil {
			category = c.portalChat(&chat, portal, category)
		}
		chat.Category = contract.Ptr(category)
		for _, st := range req.InitialState {
			parseContent(st.Type, &st.Content)
			switch v := st.Content.Parsed.(type) {
			case *event.RoomFeatures:
				chat.Features = features(v)
			case *event.RoomAvatarEventContent:
				avatar, ship := c.avatar(ctx, tx, sc.account, string(v.URL))
				chat.Avatar = avatar
				if ship != "" {
					media = append(media, ship)
				}
			case *event.RoomNameEventContent:
				chat.ChatName = contract.SetOrClear(v.Name)
			case *event.TopicEventContent:
				chat.Topic = contract.SetOrClear(v.Topic)
			}
		}
		ownerPower := 0
		if req.PowerLevelOverride != nil {
			ownerPower = req.PowerLevelOverride.UsersDefault
			if p, ok := req.PowerLevelOverride.Users[OwnerMXID]; ok {
				ownerPower = p
			}
			chat.ReadOnly = contract.Ptr(req.PowerLevelOverride.EventsDefault > ownerPower)
		}
		initial := slices.Concat(req.BeeperInitialMembers, req.Invite)
		slices.Sort(initial)
		initial = slices.Compact(initial)
		var items []contract.Member
		for _, mxid := range initial {
			user, ok := c.userOf(sc, string(mxid))
			if !ok {
				continue
			}
			if _, err := tx.SetMember(ctx, string(roomID), string(mxid), user, contract.MembershipJoin); err != nil {
				return err
			}
			item := contract.Member{User: user, Membership: contract.MembershipJoin}
			if req.PowerLevelOverride != nil {
				power := req.PowerLevelOverride.UsersDefault
				if p, ok := req.PowerLevelOverride.Users[mxid]; ok {
					power = p
				}
				if _, err := tx.SetPower(ctx, string(roomID), string(mxid), power); err != nil {
					return err
				}
				item.Role = contract.Ptr(role(power))
			}
			if err := c.ensureUser(ctx, tx, sc, user); err != nil {
				return err
			}
			items = append(items, item)
		}
		if items == nil {
			items = []contract.Member{}
		}
		for i := 0; i == 0 || i < len(items); i += maxMembersPerEvent {
			page := items[i:min(i+maxMembersPerEvent, len(items))]
			ev := &contract.ChatUpsert{Head: c.head(sc.account, contract.TypeChatUpsert, time.Time{}), Chat: chat}
			if i > 0 {
				ev.Chat = contract.Chat{ID: sc.chat}
			}
			ev.Chat.Members = &contract.Members{Complete: i == 0, Items: page}
			var m []string
			if i == 0 {
				m = media
			}
			if _, err := tx.Append(ctx, ev, m...); err != nil {
				return err
			}
		}
		return tx.SetAnnounced(ctx, string(roomID))
	})
	return roomID, err
}

// portalChat fills what a portal says about its chat and returns its
// category.
func (c *Connector) portalChat(chat *contract.Chat, portal *bridgev2.Portal, category string) string {
	switch portal.RoomType {
	case database.RoomTypeDM:
		category = contract.CategoryDirect
	case database.RoomTypeSpace:
		category = contract.CategorySpace
	}
	if c.net != nil {
		if cat := c.net.ChatCategory(portal); cat != "" {
			category = cat
		}
	}
	if portal.OtherUserID != "" && category == contract.CategoryDirect {
		chat.OtherUser = contract.Set(string(portal.OtherUserID))
	}
	chat.MessageRequest = contract.Ptr(portal.MessageRequest)
	if portal.ParentKey.ID != "" {
		chat.Parent = contract.Set(string(portal.ParentKey.ID))
	}
	if portal.Disappear.Type == event.DisappearingTypeAfterSend && portal.Disappear.Timer > 0 {
		chat.DisappearAfterSeconds = contract.Set(int64(portal.Disappear.Timer.Seconds()))
	}
	return category
}

// ensureChat sends a chat whole before anything names it, when it was not
// sent since its account was last forgotten: a link made again after the
// owner deleted the account, whose chats the server removed with it.
func (c *Connector) ensureChat(ctx context.Context, tx *store.Tx, sc *roomScope) error {
	announced, err := tx.Announced(ctx, string(sc.roomID))
	if err != nil || announced {
		return err
	}
	if err := tx.PutRoom(ctx, store.Room{RoomID: string(sc.roomID), Network: c.network, PortalID: sc.chat, Receiver: sc.account}); err != nil {
		return err
	}
	chat := contract.Chat{ID: sc.chat}
	category := contract.CategoryGroup
	var media []string
	if c.br != nil {
		if portal, _ := c.br.GetExistingPortalByKey(ctx, sc.key); portal != nil {
			category = c.portalChat(&chat, portal, category)
			chat.ChatName = contract.SetOrClear(portal.Name)
			chat.Topic = contract.SetOrClear(portal.Topic)
			if portal.AvatarMXC != "" {
				avatar, ship := c.avatar(ctx, tx, sc.account, string(portal.AvatarMXC))
				chat.Avatar = avatar
				if ship != "" {
					media = append(media, ship)
				}
			}
			if sc.login != nil && sc.login.Client != nil {
				chat.Features = features(sc.login.Client.GetCapabilities(ctx, portal))
			}
		}
	}
	chat.Category = contract.Ptr(category)
	members, err := tx.Members(ctx, string(sc.roomID))
	if err != nil {
		return err
	}
	items := []contract.Member{}
	for _, m := range members {
		if m.UserID == "" {
			continue
		}
		if err := c.ensureUser(ctx, tx, sc, m.UserID); err != nil {
			return err
		}
		items = append(items, contract.Member{User: m.UserID, Membership: m.Membership, Role: contract.Ptr(role(m.Power))})
	}
	chat.Members = &contract.Members{Complete: true, Items: items[:min(len(items), maxMembersPerEvent)]}
	if _, err := tx.Append(ctx, &contract.ChatUpsert{Head: c.head(sc.account, contract.TypeChatUpsert, time.Time{}), Chat: chat}, media...); err != nil {
		return err
	}
	for i := maxMembersPerEvent; i < len(items); i += maxMembersPerEvent {
		page := contract.Chat{ID: sc.chat, Members: &contract.Members{Items: items[i:min(i+maxMembersPerEvent, len(items))]}}
		if _, err := tx.Append(ctx, &contract.ChatUpsert{Head: c.head(sc.account, contract.TypeChatUpsert, time.Time{}), Chat: page}); err != nil {
			return err
		}
	}
	return tx.SetAnnounced(ctx, string(sc.roomID))
}

// deleteRoom appends a chat's deletion.
func (c *Connector) deleteRoom(ctx context.Context, roomID id.RoomID) error {
	return c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		if err := tx.SetRoomDeleted(ctx, string(roomID), true); err != nil {
			return err
		}
		_, err = tx.Append(ctx, &contract.ChatDelete{Head: c.head(sc.account, contract.TypeChatDelete, time.Time{}), Chat: sc.chat})
		return err
	})
}

// roomFlag records one of the owner's flags and appends it when it changed.
func (c *Connector) roomFlag(ctx context.Context, roomID id.RoomID, flag string, value int64, set func(*contract.Chat)) error {
	return c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		changed, err := tx.SetRoomFlag(ctx, string(roomID), flag, value)
		if err != nil || !changed {
			return err
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		chat := contract.Chat{ID: sc.chat}
		set(&chat)
		_, err = tx.Append(ctx, &contract.ChatUpsert{Head: c.head(sc.account, contract.TypeChatUpsert, time.Time{}), Chat: chat})
		return err
	})
}

// markRead is a read marker: the owner's own becomes chat.read, another
// user's on one of the owner's messages in a direct chat becomes its read
// status. A group's per-member receipts are left out.
func (c *Connector) markRead(ctx context.Context, in *intent, roomID id.RoomID, eventID id.EventID, ts time.Time) error {
	if in.bot {
		return nil
	}
	return c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		part, err := tx.PartByEvent(ctx, string(eventID))
		if err != nil {
			return err
		}
		if part == nil && in.owner {
			part, err = tx.NewestPart(ctx, string(roomID), ts.UnixMilli())
			if err != nil {
				return err
			}
		}
		if part == nil {
			return nil
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		if ts.IsZero() {
			ts = c.store.Now()
		}
		if in.owner {
			_, err = tx.Append(ctx, &contract.ChatRead{Head: c.head(sc.account, contract.TypeChatRead, ts), Chat: sc.chat, UpTo: part.MessageID})
			return err
		}
		if !part.FromOwner || !c.isDirect(ctx, sc) {
			return nil
		}
		_, err = tx.Append(ctx, &contract.MessageStatus{
			Head: c.head(sc.account, contract.TypeMessageStatus, ts),
			Chat: sc.chat, Message: part.MessageID, Status: contract.StatusRead,
		})
		return err
	})
}

func (c *Connector) isDirect(ctx context.Context, sc *roomScope) bool {
	if c.br == nil {
		return false
	}
	portal, err := c.br.GetExistingPortalByKey(ctx, sc.key)
	return err == nil && portal != nil && portal.RoomType == database.RoomTypeDM
}

// deliveryReceipt is one of the owner's messages delivered.
func (c *Connector) deliveryReceipt(ctx context.Context, info *bridgev2.MessageStatusEventInfo) {
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, info.RoomID)
		if err != nil || sc == nil {
			return err
		}
		part, err := tx.PartByEvent(ctx, string(info.SourceEventID))
		if err != nil || part == nil || !part.FromOwner {
			return err
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		_, err = tx.Append(ctx, &contract.MessageStatus{
			Head: c.head(sc.account, contract.TypeMessageStatus, c.store.Now()),
			Chat: sc.chat, Message: part.MessageID, Status: contract.StatusDelivered,
		})
		return err
	})
	if err != nil {
		c.log.Error("recording a delivery receipt failed", "error", err)
	}
}

// batch stores a page of history and appends each message, then the
// reactions, then where history stands.
func (c *Connector) batch(ctx context.Context, roomID id.RoomID, req *mautrix.ReqBeeperBatchSend, extras []*bridgev2.MatrixSendExtra) error {
	return c.store.Do(ctx, func(tx *store.Tx) error {
		sc, err := c.scope(ctx, tx, roomID)
		if err != nil || sc == nil {
			return err
		}
		choose := c.chooser(ctx, tx, sc.account)
		var order, reacted []string
		seen := map[string]bool{}
		var oldest, newest *store.Part
		for i, evt := range req.Events {
			var extra *bridgev2.MatrixSendExtra
			if i < len(extras) {
				extra = extras[i]
			}
			if extra == nil {
				continue
			}
			ts := time.UnixMilli(evt.Timestamp)
			switch {
			case extra.MessageMeta != nil:
				meta := extra.MessageMeta
				f := foldContent(evt.Type, &evt.Content, func(m string) (string, bool) { return c.userOf(sc, m) }, choose)
				f.ReplyTo = string(meta.ReplyTo.MessageID)
				f.ThreadRoot = string(meta.ThreadRoot)
				raw, err := json.Marshal(f)
				if err != nil {
					return err
				}
				part := store.Part{
					EventID:   string(evt.ID),
					RoomID:    string(roomID),
					MessageID: string(meta.ID),
					PartID:    string(meta.PartID),
					Index:     extra.PartIndex,
					Sender:    string(meta.SenderID),
					FromOwner: evt.Sender == OwnerMXID || c.isSelf(ctx, sc, meta.SenderID),
					TS:        ts.UnixMilli(),
					Content:   string(raw),
				}
				if err := tx.PutPart(ctx, part); err != nil {
					return err
				}
				if !seen[part.MessageID] {
					seen[part.MessageID] = true
					order = append(order, part.MessageID)
				}
				if oldest == nil || part.TS < oldest.TS {
					oldest = &part
				}
				if newest == nil || part.TS >= newest.TS {
					newest = &part
				}
			case extra.ReactionMeta != nil:
				meta := extra.ReactionMeta
				parseContent(event.EventReaction, &evt.Content)
				key := meta.Emoji
				if rc, ok := evt.Content.Parsed.(*event.ReactionEventContent); ok && rc.RelatesTo.Key != "" {
					key = rc.RelatesTo.Key
				}
				if err := tx.PutReaction(ctx, store.ReactionRow{
					EventID: string(evt.ID), RoomID: string(roomID), MessageID: string(meta.MessageID),
					Sender: string(meta.SenderID), EmojiID: string(meta.EmojiID), Key: reactionKey(key), TS: ts.UnixMilli(),
				}); err != nil {
					return err
				}
				if !slices.Contains(reacted, string(meta.MessageID)) {
					reacted = append(reacted, string(meta.MessageID))
				}
			}
		}
		for _, msgID := range order {
			if err := c.emitMessage(ctx, tx, sc, msgID, true); err != nil {
				return err
			}
		}
		for _, msgID := range reacted {
			if err := c.emitReactions(ctx, tx, sc, msgID); err != nil {
				return err
			}
		}
		if err := c.ensureChat(ctx, tx, sc); err != nil {
			return err
		}
		if req.MarkReadBy == OwnerMXID && newest != nil {
			if _, err := tx.Append(ctx, &contract.ChatRead{
				Head: c.head(sc.account, contract.TypeChatRead, time.UnixMilli(newest.TS)),
				Chat: sc.chat, UpTo: newest.MessageID,
			}); err != nil {
				return err
			}
		}
		if !req.Forward && oldest != nil {
			ev := &contract.BackfillProgress{
				Head:     c.head(sc.account, contract.TypeBackfillProgress, time.Time{}),
				Chat:     contract.Ptr(sc.chat),
				OldestAt: contract.AtPtr(time.UnixMilli(oldest.TS)),
			}
			if _, err := tx.Append(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	})
}

// ghostProfile records a change to a user's profile and resends the user
// to every account that was sent them.
func (c *Connector) ghostProfile(ctx context.Context, user networkid.UserID, update func(*store.Ghost)) error {
	if user == "" {
		return nil
	}
	return c.store.Do(ctx, func(tx *store.Tx) error {
		g, err := tx.Ghost(ctx, c.network, string(user))
		if err != nil {
			return err
		}
		if g == nil {
			g = &store.Ghost{Network: c.network, UserID: string(user)}
		}
		update(g)
		if err := tx.PutGhost(ctx, *g); err != nil {
			return err
		}
		accounts, err := tx.AccountsSeeing(ctx, c.network, string(user))
		if err != nil {
			return err
		}
		for _, account := range accounts {
			sc := &roomScope{account: account}
			if c.br != nil {
				sc.login = c.br.GetCachedUserLoginByID(networkid.UserLoginID(account))
			}
			if err := c.ensureUser(ctx, tx, sc, string(user)); err != nil {
				return err
			}
		}
		return nil
	})
}

func newEventID() id.EventID {
	return id.EventID("$" + store.RandomID(18) + ":" + ServerName)
}
