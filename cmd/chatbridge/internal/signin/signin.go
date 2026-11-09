// Package signin drives a network's bridgev2 login flow from a terminal on
// the bridge's host (phase 1 of docs/plans/chat-bridge.md, section 8): a QR
// drawn in the terminal, or a pairing code to type on the phone. Nothing of
// a sign-in crosses the server; it learns of the account from account.upsert
// and account.state.
package signin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"maunium.net/go/mautrix/bridgev2"
)

// Terminal is where a sign-in talks to the owner.
type Terminal struct {
	In  io.Reader
	Out io.Writer
	// Answers fill input fields before the terminal asks, by the field's id
	// or by its type (phone_number), which is the same on every network.
	Answers map[string]string
	// InvertQR draws the QR for a light terminal background.
	InvertQR bool
	// Secret reads a password or a token without echoing it. Without one,
	// such a field is read like any other.
	Secret func(prompt string) (string, error)
}

// ErrNotAcknowledged is a link the owner did not agree to.
var ErrNotAcknowledged = errors.New("not linked: the network's rule was not acknowledged")

// Acknowledge shows a rule of the network the bridge breaks and goes on
// only when the owner types yes.
func (t Terminal) Acknowledge(rule string) error {
	fmt.Fprintf(t.Out, "%s\n\nType yes to link the account anyway: ", rule)
	line, err := bufio.NewReader(t.In).ReadString('\n')
	if strings.EqualFold(strings.TrimSpace(line), "yes") {
		return nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return ErrNotAcknowledged
}

// secret reports a field whose value must not be echoed.
func secret(t bridgev2.LoginInputFieldType) bool {
	return t == bridgev2.LoginInputFieldTypePassword || t == bridgev2.LoginInputFieldTypeToken
}

// Run drives one login flow to its end and returns the new login.
func Run(ctx context.Context, br *bridgev2.Bridge, user *bridgev2.User, flowID string, term Terminal) (*bridgev2.UserLogin, error) {
	proc, err := br.Network.CreateLogin(ctx, user, flowID)
	if err != nil {
		return nil, err
	}
	step, err := proc.Start(ctx)
	if err != nil {
		proc.Cancel()
		return nil, err
	}
	reader := bufio.NewReader(term.In)
	for {
		if err := ctx.Err(); err != nil {
			proc.Cancel()
			return nil, err
		}
		switch step.Type {
		case bridgev2.LoginStepTypeUserInput:
			input, ok := proc.(bridgev2.LoginProcessUserInput)
			if !ok {
				proc.Cancel()
				return nil, errors.New("the network asked for input it cannot take")
			}
			values, askErr := ask(term, reader, step)
			if askErr != nil {
				proc.Cancel()
				return nil, askErr
			}
			step, err = input.SubmitUserInput(ctx, values)
		case bridgev2.LoginStepTypeDisplayAndWait:
			wait, ok := proc.(bridgev2.LoginProcessDisplayAndWait)
			if !ok {
				proc.Cancel()
				return nil, errors.New("the network asked to wait without a way to")
			}
			show(term, step)
			step, err = wait.Wait(ctx)
		case bridgev2.LoginStepTypeComplete:
			if step.CompleteParams == nil {
				return nil, errors.New("the login completed without naming the account")
			}
			if step.CompleteParams.UserLogin != nil {
				return step.CompleteParams.UserLogin, nil
			}
			login := br.GetCachedUserLoginByID(step.CompleteParams.UserLoginID)
			if login == nil {
				return nil, errors.New("the login completed but the account is not loaded")
			}
			return login, nil
		default:
			proc.Cancel()
			return nil, fmt.Errorf("the network asked for a %s step, which a terminal sign-in cannot take; the remote sign-in comes later", step.Type)
		}
		if err != nil {
			return nil, err
		}
	}
}

func ask(term Terminal, reader *bufio.Reader, step *bridgev2.LoginStep) (map[string]string, error) {
	if step.Instructions != "" {
		fmt.Fprintln(term.Out, step.Instructions)
	}
	values := map[string]string{}
	if step.UserInputParams == nil {
		return values, nil
	}
	for _, f := range step.UserInputParams.Fields {
		if v := term.Answers[f.ID]; v != "" {
			values[f.ID] = v
			continue
		}
		if v := term.Answers[string(f.Type)]; v != "" {
			values[f.ID] = v
			continue
		}
		prompt := f.Name + ": "
		if f.Description != "" {
			prompt = fmt.Sprintf("%s (%s): ", f.Name, f.Description)
		}
		if secret(f.Type) && term.Secret != nil {
			v, err := term.Secret(prompt)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", f.Name, err)
			}
			values[f.ID] = strings.TrimSpace(v)
			continue
		}
		fmt.Fprint(term.Out, prompt)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return nil, fmt.Errorf("reading %s: %w", f.Name, err)
		}
		values[f.ID] = strings.TrimSpace(line)
	}
	return values, nil
}

func show(term Terminal, step *bridgev2.LoginStep) {
	p := step.DisplayAndWaitParams
	if p == nil {
		return
	}
	if step.Instructions != "" {
		fmt.Fprintln(term.Out, step.Instructions)
	}
	switch p.Type {
	case bridgev2.LoginDisplayTypeQR:
		qr, err := QR(p.Data, term.InvertQR)
		if err != nil {
			fmt.Fprintf(term.Out, "The QR could not be drawn (%v); its data is:\n%s\n", err, p.Data)
			return
		}
		fmt.Fprint(term.Out, qr)
	case bridgev2.LoginDisplayTypeCode, bridgev2.LoginDisplayTypeEmoji:
		fmt.Fprintf(term.Out, "\n    %s\n\n", p.Data)
	case bridgev2.LoginDisplayTypeNothing:
	}
}

// QR draws data as a QR code in half-block characters, two modules a line,
// light modules as blocks for a dark terminal unless invert.
func QR(data string, invert bool) (string, error) {
	code, err := qrcode.New(data, qrcode.Low)
	if err != nil {
		return "", err
	}
	bm := code.Bitmap()
	dark := func(y, x int) bool {
		if y >= len(bm) {
			return invert
		}
		return bm[y][x] != invert
	}
	var b strings.Builder
	for y := 0; y < len(bm); y += 2 {
		for x := range bm[y] {
			top, bottom := !dark(y, x), !dark(y+1, x)
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}
