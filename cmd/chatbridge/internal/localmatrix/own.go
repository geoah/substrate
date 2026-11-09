package localmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// The actions the owner can ask for.
const (
	ActionSend     = "send"
	ActionReact    = "react"
	ActionUnreact  = "unreact"
	ActionEdit     = "edit"
	ActionDelete   = "delete"
	ActionMarkRead = "markread"
)

// Request is one action, resolved to network ids.
type Request struct {
	Action      string `json:"action"`
	Kind        string `json:"kind"`
	Account     string `json:"account"`
	Chat        string `json:"chat"`
	Target      string `json:"target,omitempty"`
	ReplyTo     string `json:"replyTo,omitempty"`
	Text        string `json:"text,omitempty"`
	Key         string `json:"key,omitempty"`
	ForEveryone bool   `json:"forEveryone,omitempty"`
	// Txn is the transaction id the network sees: the action's own id in
	// the network's format, so a network that deduplicates by sender-chosen
	// id deduplicates a repeat of this action.
	Txn string `json:"txn"`
}

// Outcome is how one execution ended.
type Outcome struct {
	Outcome string
	// Message is the network's id of the message an action sent.
	Message   string
	ErrorCode string
	ErrorText string
}

func failed(code, text string) Outcome {
	return Outcome{Outcome: contract.OutcomeFailed, ErrorCode: code, ErrorText: text}
}

// eventID is the synthetic Matrix event an action becomes. Its id carries
// the transaction id to bridgev2 (database.NetworkTxnMXIDPrefix), which
// hands it to the connector as the input transaction id.
func eventID(txn string) id.EventID {
	return id.EventID(database.NetworkTxnMXIDPrefix + txn)
}

// portal finds the portal of an action's chat, with its room.
func (c *Connector) portal(ctx context.Context, account, chat string) (*bridgev2.Portal, error) {
	if c.br == nil {
		return nil, errors.New("the bridge is not running")
	}
	portal, err := c.br.GetExistingPortalByKey(ctx, networkid.PortalKey{ID: networkid.PortalID(chat), Receiver: networkid.UserLoginID(account)})
	if err != nil {
		return nil, err
	}
	if portal == nil || portal.MXID == "" {
		return nil, nil
	}
	return portal, nil
}

// ChatInfo is what the rules read about a chat before an action is
// claimed.
type ChatInfo struct {
	Exists bool
	Direct bool
	// MessageRequest is a chat the other side started that the owner has
	// not accepted.
	MessageRequest bool
	// OtherIsContact says whether a direct chat's other user is in the
	// owner's address book, when the network can tell (ContactKnown).
	OtherIsContact bool
	ContactKnown   bool
	OwnerSent      bool
	Features       *event.RoomFeatures
}

// Inspect reads a chat for the rules.
func (c *Connector) Inspect(ctx context.Context, account, chat string) (ChatInfo, error) {
	var info ChatInfo
	portal, err := c.portal(ctx, account, chat)
	if err != nil || portal == nil {
		return info, err
	}
	info.Exists = true
	info.Direct = portal.RoomType == database.RoomTypeDM
	info.MessageRequest = portal.MessageRequest
	login := c.br.GetCachedUserLoginByID(networkid.UserLoginID(account))
	if login != nil && login.Client != nil {
		info.Features = login.Client.GetCapabilities(ctx, portal)
		if info.Direct && portal.OtherUserID != "" && c.net != nil {
			x := c.net.UserExtras(ctx, login, portal.OtherUserID)
			info.ContactKnown, info.OtherIsContact = x.Known, x.IsContact
		}
	}
	err = c.store.View(ctx, func(tx *store.Tx) error {
		var err error
		info.OwnerSent, err = tx.OwnerSent(ctx, string(portal.MXID))
		return err
	})
	return info, err
}

// targetPart is the first part of a message bridgev2 holds.
func (c *Connector) targetPart(ctx context.Context, account, messageID string) (*database.Message, error) {
	return c.br.DB.Message.GetFirstPartByID(ctx, networkid.UserLoginID(account), networkid.MessageID(messageID))
}

// MessageAge is when a message was sent, for the edit and delete windows.
func (c *Connector) MessageAge(ctx context.Context, account, messageID string) (time.Time, bool, error) {
	if c.br == nil {
		return time.Time{}, false, nil
	}
	part, err := c.targetPart(ctx, account, messageID)
	if err != nil || part == nil {
		return time.Time{}, false, err
	}
	return part.Timestamp, true, nil
}

// Execute runs one action through bridgev2 as a Matrix event from the
// owner and reads its outcome. It never retries: an outcome it cannot be
// sure of is uncertain.
func (c *Connector) Execute(ctx context.Context, req Request) Outcome {
	portal, err := c.portal(ctx, req.Account, req.Chat)
	if err != nil {
		return failed(contract.ErrNetwork, err.Error())
	}
	if portal == nil {
		return failed(contract.ErrNotFound, "the chat is not known to this bridge")
	}
	evt := &event.Event{
		ID:        eventID(req.Txn),
		RoomID:    portal.MXID,
		Sender:    OwnerMXID,
		Timestamp: c.store.Now().UnixMilli(),
	}
	switch req.Kind {
	case ActionSend:
		content := format.RenderMarkdown(req.Text, true, false)
		if req.ReplyTo != "" {
			target, err := c.targetPart(ctx, req.Account, req.ReplyTo)
			if err != nil {
				return failed(contract.ErrNetwork, err.Error())
			}
			if target == nil {
				return failed(contract.ErrNotFound, "the message to reply to is not known to this bridge")
			}
			content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(target.MXID)
		}
		evt.Type = event.EventMessage
		evt.Content = event.Content{Parsed: &content}
	case ActionEdit:
		target, err := c.targetPart(ctx, req.Account, req.Target)
		if err != nil {
			return failed(contract.ErrNetwork, err.Error())
		}
		if target == nil {
			return failed(contract.ErrNotFound, "the message to edit is not known to this bridge")
		}
		newContent := format.RenderMarkdown(req.Text, true, false)
		content := &event.MessageEventContent{MsgType: event.MsgText, Body: "* " + newContent.Body, NewContent: &newContent}
		content.RelatesTo = (&event.RelatesTo{}).SetReplace(target.MXID)
		evt.Type = event.EventMessage
		evt.Content = event.Content{Parsed: content}
	case ActionReact:
		target, err := c.targetPart(ctx, req.Account, req.Target)
		if err != nil {
			return failed(contract.ErrNetwork, err.Error())
		}
		if target == nil {
			return failed(contract.ErrNotFound, "the message to react to is not known to this bridge")
		}
		evt.Type = event.EventReaction
		evt.Content = event.Content{Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
			Type: event.RelAnnotation, EventID: target.MXID, Key: variationselector.Add(req.Key),
		}}}
	case ActionUnreact:
		reaction, err := c.ownReaction(ctx, portal, req)
		if err != nil {
			return failed(contract.ErrNetwork, err.Error())
		}
		if reaction == nil {
			return failed(contract.ErrNotFound, "the owner has no such reaction on the message")
		}
		evt.Type = event.EventRedaction
		evt.Redacts = reaction.MXID
		evt.Content = event.Content{Parsed: &event.RedactionEventContent{Redacts: reaction.MXID}}
	case ActionDelete:
		if !req.ForEveryone {
			return failed(contract.ErrUnsupported, "deleting a message only for the owner is not supported")
		}
		target, err := c.targetPart(ctx, req.Account, req.Target)
		if err != nil {
			return failed(contract.ErrNetwork, err.Error())
		}
		if target == nil {
			return failed(contract.ErrNotFound, "the message to delete is not known to this bridge")
		}
		evt.Type = event.EventRedaction
		evt.Redacts = target.MXID
		evt.Content = event.Content{Parsed: &event.RedactionEventContent{Redacts: target.MXID}}
	case ActionMarkRead:
		var target *database.Message
		if req.Target != "" {
			target, err = c.targetPart(ctx, req.Account, req.Target)
		} else {
			target, err = c.br.DB.Message.GetLastNonFakePartAtOrBeforeTime(ctx, portal.PortalKey, c.store.Now())
		}
		if err != nil {
			return failed(contract.ErrNetwork, err.Error())
		}
		if target == nil {
			return failed(contract.ErrNotFound, "there is no message to mark read up to")
		}
		evt.ID = ""
		evt.Type = event.EphemeralEventReceipt
		// bridgev2 reads a receipt only from an ephemeral source.
		evt.Mautrix.EventSource = event.SourceEphemeral
		evt.Content = event.Content{Parsed: &event.ReceiptEventContent{
			target.MXID: {event.ReceiptTypeRead: {OwnerMXID: {Timestamp: c.store.Now()}}},
		}}
	default:
		return failed(contract.ErrInvalid, fmt.Sprintf("no action %q", req.Kind))
	}
	if raw, err := json.Marshal(evt.Content.Parsed); err == nil {
		evt.Content.VeryRaw = raw
	}
	if req.Kind == ActionMarkRead && c.net != nil {
		c.net.BeforeRead(ctx, c.br.GetCachedUserLoginByID(portal.Receiver), portal)
	}
	res := c.br.QueueMatrixEvent(ctx, evt)
	if res.Error != nil && c.net != nil {
		if w, ok := c.net.Throttle(res.Error, c.store.Now()); ok {
			c.warn(ctx, req.Account, w)
		}
	}
	return c.outcome(ctx, portal, req, evt, res)
}

// outcome reads bridgev2's handling result.
func (c *Connector) outcome(ctx context.Context, portal *bridgev2.Portal, req Request, evt *event.Event, res bridgev2.EventHandlingResult) Outcome {
	switch {
	case res.Error != nil && errors.Is(res.Error, bridgev2.ErrHandlerBackgrounded):
		return Outcome{
			Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork,
			ErrorText: "the network did not answer in time; the action may or may not have been carried out",
		}
	case res.Error != nil:
		return c.classify(res.Error)
	case !res.Success && !res.Ignored:
		return failed(contract.ErrRejected, "the bridge refused the action")
	}
	out := Outcome{Outcome: contract.OutcomeDone}
	if req.Kind == ActionSend {
		var msg *database.Message
		if res.EventID != "" {
			msg, _ = c.br.DB.Message.GetPartByMXID(ctx, res.EventID)
		}
		if msg == nil {
			msg, _ = c.sentMessage(ctx, portal, req)
		}
		if msg != nil {
			out.Message = string(msg.ID)
		}
	}
	return out
}

// classify maps a failure to an outcome: a refusal before anything left is
// failed, a network that stopped answering mid-send is uncertain.
func (c *Connector) classify(err error) Outcome {
	if c.net != nil {
		if o, ok := c.net.Classify(err); ok {
			return o
		}
	}
	switch {
	case errors.Is(err, bridgev2.ErrTargetMessageNotFound), errors.Is(err, bridgev2.ErrNoPortal):
		return failed(contract.ErrNotFound, err.Error())
	case errors.Is(err, bridgev2.ErrEditsNotSupported), errors.Is(err, bridgev2.ErrEditsNotSupportedInPortal),
		errors.Is(err, bridgev2.ErrReactionsNotSupported), errors.Is(err, bridgev2.ErrRedactionsNotSupported),
		errors.Is(err, bridgev2.ErrUnsupportedMessageType), errors.Is(err, bridgev2.ErrUnsupportedMediaType):
		return failed(contract.ErrUnsupported, err.Error())
	case errors.Is(err, bridgev2.ErrEditTargetTooOld), errors.Is(err, bridgev2.ErrEditTargetTooManyEdits):
		return failed(contract.ErrRejected, err.Error())
	case errors.Is(err, bridgev2.ErrRemoteEchoTimeout), errors.Is(err, bridgev2.ErrRemoteAckTimeout),
		errors.Is(err, context.DeadlineExceeded):
		return Outcome{Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork, ErrorText: err.Error()}
	}
	var ms bridgev2.MessageStatus
	if errors.As(err, &ms) {
		switch ms.ErrorReason {
		case event.MessageStatusUnsupported:
			return failed(contract.ErrUnsupported, err.Error())
		case event.MessageStatusNoPermission:
			return failed(contract.ErrRejected, err.Error())
		case event.MessageStatusTooOld:
			return failed(contract.ErrExpired, err.Error())
		}
		if !ms.IsCertain && ms.ErrorReason == event.MessageStatusNetworkError {
			return Outcome{Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork, ErrorText: err.Error()}
		}
	}
	if errors.Is(err, bridgev2.ErrNotLoggedIn) {
		return failed(contract.ErrNetwork, err.Error())
	}
	return failed(contract.ErrRejected, err.Error())
}

// sentMessage finds the message an action sent, by the ids its transaction
// id gives it on the network.
func (c *Connector) sentMessage(ctx context.Context, portal *bridgev2.Portal, req Request) (*database.Message, error) {
	if c.net == nil {
		return nil, nil
	}
	login := c.br.GetCachedUserLoginByID(portal.Receiver)
	for _, id := range c.net.SentMessageIDs(login, req.Chat, req.Txn) {
		msg, err := c.br.DB.Message.GetFirstPartByID(ctx, portal.Receiver, id)
		if err != nil || msg != nil {
			return msg, err
		}
	}
	return nil, nil
}

// ownReaction finds the owner's reaction with an action's key.
func (c *Connector) ownReaction(ctx context.Context, portal *bridgev2.Portal, req Request) (*database.Reaction, error) {
	reactions, err := c.br.DB.Reaction.GetAllToMessage(ctx, portal.Receiver, networkid.MessageID(req.Target))
	if err != nil {
		return nil, err
	}
	login := c.br.GetCachedUserLoginByID(portal.Receiver)
	key := reactionKey(req.Key)
	for _, r := range reactions {
		mine := login != nil && login.Client != nil && login.Client.IsThisUser(ctx, r.SenderID)
		if mine && (reactionKey(r.Emoji) == key || string(r.EmojiID) == key) {
			return r, nil
		}
	}
	return nil, nil
}

// FindSent looks for what an action did, for a restart that found a claim
// that left for the network and no outcome. found is false when the network
// cannot tell, which makes the action uncertain.
func (c *Connector) FindSent(ctx context.Context, req Request) (Outcome, bool) {
	portal, err := c.portal(ctx, req.Account, req.Chat)
	if err != nil || portal == nil {
		return Outcome{}, false
	}
	switch req.Kind {
	case ActionSend:
		msg, err := c.sentMessage(ctx, portal, req)
		if err != nil || msg == nil {
			return Outcome{}, false
		}
		return Outcome{Outcome: contract.OutcomeDone, Message: string(msg.ID)}, true
	case ActionReact:
		r, err := c.ownReaction(ctx, portal, req)
		if err != nil || r == nil {
			return Outcome{}, false
		}
		return Outcome{Outcome: contract.OutcomeDone}, true
	}
	return Outcome{}, false
}

// Echo writes what a done action changed into the mirror and appends it,
// inside the transaction that records the outcome: the owner's message
// comes back carrying the action, so the server links them.
func (c *Connector) Echo(ctx context.Context, tx *store.Tx, req Request, out Outcome) error {
	if out.Outcome != contract.OutcomeDone || c.br == nil {
		return nil
	}
	portal, err := c.portal(ctx, req.Account, req.Chat)
	if err != nil || portal == nil {
		return err
	}
	sc, err := c.scope(ctx, tx, portal.MXID)
	if err != nil {
		return err
	}
	if sc == nil {
		if err := tx.PutRoom(ctx, store.Room{RoomID: string(portal.MXID), Network: c.network, PortalID: req.Chat, Receiver: req.Account}); err != nil {
			return err
		}
		if sc, err = c.scope(ctx, tx, portal.MXID); err != nil || sc == nil {
			return err
		}
	}
	now := c.store.Now()
	switch req.Kind {
	case ActionSend:
		var msg *database.Message
		if out.Message != "" {
			msg, err = c.targetPart(ctx, req.Account, out.Message)
		} else {
			msg, err = c.sentMessage(ctx, portal, req)
		}
		if err != nil || msg == nil {
			return err
		}
		content := format.RenderMarkdown(req.Text, true, false)
		f := foldContent(event.EventMessage, &event.Content{Parsed: &content}, func(m string) (string, bool) { return c.userOf(sc, m) }, nil)
		f.ReplyTo = req.ReplyTo
		f.Text = req.Text
		raw, err := json.Marshal(f)
		if err != nil {
			return err
		}
		ts := msg.Timestamp
		if ts.IsZero() {
			ts = now
		}
		if err := tx.PutPart(ctx, store.Part{
			EventID: string(msg.MXID), RoomID: string(portal.MXID), MessageID: string(msg.ID), PartID: string(msg.PartID),
			Sender: string(msg.SenderID), FromOwner: true, TS: ts.UnixMilli(), Content: string(raw), Action: req.Action,
		}); err != nil {
			return err
		}
		return c.emitMessage(ctx, tx, sc, string(msg.ID), false)
	case ActionEdit:
		target, err := c.targetPart(ctx, req.Account, req.Target)
		if err != nil || target == nil {
			return err
		}
		content := format.RenderMarkdown(req.Text, true, false)
		f := foldContent(event.EventMessage, &event.Content{Parsed: &content}, func(m string) (string, bool) { return c.userOf(sc, m) }, nil)
		f.Text = req.Text
		meta := &database.Message{ID: target.ID, PartID: target.PartID}
		edited, err := c.editPart(ctx, tx, sc, meta, f, now)
		if err != nil || edited {
			return err
		}
		return nil
	case ActionReact, ActionUnreact:
		return c.resyncReactions(ctx, tx, sc, portal, req.Target)
	case ActionDelete:
		changed, err := tx.MarkMessageDeleted(ctx, string(portal.MXID), req.Target, contract.ScopeEveryone)
		if err != nil || !changed {
			return err
		}
		ev := &contract.MessageDelete{
			Head: c.head(sc.account, contract.TypeMessageDelete, now), Chat: sc.chat,
			Message: req.Target, Scope: contract.ScopeEveryone,
		}
		if self := c.selfUser(sc); self != "" {
			ev.By = contract.Ptr(self)
		}
		_, err = tx.Append(ctx, ev)
		return err
	case ActionMarkRead:
		upTo := req.Target
		if upTo == "" {
			part, err := tx.NewestPart(ctx, string(portal.MXID), 0)
			if err != nil || part == nil {
				return err
			}
			upTo = part.MessageID
		}
		_, err := tx.Append(ctx, &contract.ChatRead{Head: c.head(sc.account, contract.TypeChatRead, now), Chat: sc.chat, UpTo: upTo})
		return err
	}
	return nil
}

// resyncReactions replaces the mirror's reactions on a message with the set
// bridgev2 holds and appends it.
func (c *Connector) resyncReactions(ctx context.Context, tx *store.Tx, sc *roomScope, portal *bridgev2.Portal, messageID string) error {
	reactions, err := c.br.DB.Reaction.GetAllToMessage(ctx, portal.Receiver, networkid.MessageID(messageID))
	if err != nil {
		return err
	}
	rows := make([]store.ReactionRow, 0, len(reactions))
	for _, r := range reactions {
		key := r.Emoji
		if key == "" {
			key = string(r.EmojiID)
		}
		evtID := r.MXID
		if evtID == "" || strings.HasPrefix(string(evtID), database.TxnMXIDPrefix) {
			evtID = c.GenerateReactionEventID(portal.MXID, &database.Message{ID: r.MessageID, PartID: r.MessagePartID}, r.SenderID, r.EmojiID)
		}
		rows = append(rows, store.ReactionRow{
			EventID: string(evtID), RoomID: string(portal.MXID), MessageID: messageID,
			Sender: string(r.SenderID), EmojiID: string(r.EmojiID), Key: reactionKey(key), TS: r.Timestamp.UnixMilli(),
		})
	}
	if err := tx.ReplaceReactions(ctx, string(portal.MXID), messageID, rows); err != nil {
		return err
	}
	return c.emitReactions(ctx, tx, sc, messageID)
}
