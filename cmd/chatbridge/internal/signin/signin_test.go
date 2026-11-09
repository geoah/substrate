package signin

import (
	"bufio"
	"bytes"
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
