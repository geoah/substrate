package signin

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
)

func TestSecretsAreReadWithoutEchoAndAnswersMatchByType(t *testing.T) {
	var out bytes.Buffer
	var asked []string
	term := Terminal{
		Out:     &out,
		Answers: map[string]string{"phone_number": "+447700900123"},
		Secret: func(prompt string) (string, error) {
			asked = append(asked, prompt)
			return "hunter2\n", nil
		},
	}
	step := &bridgev2.LoginStep{Type: bridgev2.LoginStepTypeUserInput, UserInputParams: &bridgev2.LoginUserInputParams{
		Fields: []bridgev2.LoginInputDataField{
			{Type: bridgev2.LoginInputFieldTypePhoneNumber, ID: "fi.mau.telegram.login.phone_number", Name: "Phone number"},
			{Type: bridgev2.LoginInputFieldType2FACode, ID: "code", Name: "Code"},
			{Type: bridgev2.LoginInputFieldTypePassword, ID: "password", Name: "Password"},
		},
	}}
	values, err := ask(term, bufio.NewReader(strings.NewReader("12345\n")), step)
	if err != nil {
		t.Fatal(err)
	}
	if values["fi.mau.telegram.login.phone_number"] != "+447700900123" || values["code"] != "12345" || values["password"] != "hunter2" {
		t.Fatalf("values %v", values)
	}
	if len(asked) != 1 || strings.Contains(out.String(), "hunter2") {
		t.Fatalf("the password was echoed or not asked for as a secret: %q, %v", out.String(), asked)
	}
}

func TestTheQRDrawsHalfBlocks(t *testing.T) {
	qr, err := QR("https://example.com/link", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsAny(qr, "█▀▄") || strings.Count(qr, "\n") < 10 {
		t.Fatalf("qr %q", qr)
	}
}

func TestALinkTheNetworkForbidsTakesAnExplicitYes(t *testing.T) {
	for input, ok := range map[string]bool{"yes\n": true, " YES \n": true, "yes": true, "y\n": false, "\n": false, "": false, "no\n": false} {
		var out bytes.Buffer
		err := Terminal{In: strings.NewReader(input), Out: &out}.Acknowledge("Discord's rule: no automating.")
		if (err == nil) != ok || (!ok && !errors.Is(err, ErrNotAcknowledged)) {
			t.Fatalf("%q reads %v", input, err)
		}
		if !strings.Contains(out.String(), "Discord's rule: no automating.") {
			t.Fatalf("the rule was not shown: %q", out.String())
		}
	}
	// What follows the yes stays for the sign-in, as a piped token does.
	in := bufio.NewReader(strings.NewReader("yes\ntoken-line\n"))
	if err := (Terminal{In: in, Out: &bytes.Buffer{}}).Acknowledge("rule"); err != nil {
		t.Fatal(err)
	}
	if rest, _ := bufio.NewReader(in).ReadString('\n'); rest != "token-line\n" {
		t.Fatalf("the acknowledgment ate what followed: %q", rest)
	}
}
