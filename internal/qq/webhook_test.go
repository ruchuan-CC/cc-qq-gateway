package qq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "dummy-bot-secret-1234"

// TestWebhookValidationRoundTrip verifies the op-13 challenge is answered with a
// signature that validates against the derived public key.
func TestWebhookValidationRoundTrip(t *testing.T) {
	s := NewWebhookServer(testSecret, "/qqbot", nil, nil)

	plainToken := "Arq0D5A61EgUu4OxUvOp"
	eventTs := "1725442341"

	body, _ := json.Marshal(Payload{
		Op:   OpCallbackValidation,
		Data: mustJSON(CallbackValidation{PlainToken: plainToken, EventTs: eventTs}),
	})

	req := httptest.NewRequest(http.MethodPost, "/qqbot", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp CallbackValidationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PlainToken != plainToken {
		t.Errorf("plain_token = %q, want %q", resp.PlainToken, plainToken)
	}
	sig, err := hex.DecodeString(resp.Signature)
	if err != nil {
		t.Fatalf("signature not hex: %v", err)
	}
	// The signature is over event_ts + plain_token and must verify with the pub key.
	if !s.Verify(eventTs, []byte(plainToken), sig) {
		t.Errorf("validation signature did not verify")
	}
}

// TestWebhookSignatureVerification verifies that a push signed by the bot's
// private key (timestamp+body) is accepted and a tampered one is rejected.
func TestWebhookSignatureVerification(t *testing.T) {
	s := NewWebhookServer(testSecret, "/qqbot", nil, nil)

	_, priv := deriveEd25519(testSecret)
	timestamp := "1700000000"
	body := []byte(`{"op":0,"t":"C2C_MESSAGE_CREATE","d":{}}`)

	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	good := ed25519.Sign(priv, msg.Bytes())

	if !s.Verify(timestamp, body, good) {
		t.Errorf("valid signature rejected")
	}
	if s.Verify(timestamp, append(append([]byte{}, body...), '!'), good) {
		t.Errorf("tampered body accepted")
	}
	if s.Verify("9999999999", body, good) {
		t.Errorf("wrong timestamp accepted")
	}
}

// TestWebhookEventDispatch verifies a correctly-signed event reaches the handler.
func TestWebhookEventDispatch(t *testing.T) {
	var got *Payload
	s := NewWebhookServer(testSecret, "/qqbot", func(_ context.Context, p *Payload) { got = p }, nil)

	_, priv := deriveEd25519(testSecret)
	body := []byte(`{"id":"evt-1","op":0,"s":5,"t":"C2C_MESSAGE_CREATE","d":{"id":"abc","content":"hi"}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10) // fresh — passes the skew check
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	sig := hex.EncodeToString(ed25519.Sign(priv, msg.Bytes()))

	req := httptest.NewRequest(http.MethodPost, "/qqbot", bytes.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got == nil || got.Type != EventC2CMessageCreate {
		t.Fatalf("handler did not receive the event; got %+v", got)
	}
}

// TestWebhookRejectsStalePush verifies the timestamp-skew replay guard.
func TestWebhookRejectsStalePush(t *testing.T) {
	s := NewWebhookServer(testSecret, "/qqbot", func(context.Context, *Payload) {}, nil)
	_, priv := deriveEd25519(testSecret)
	body := []byte(`{"id":"evt-2","op":0,"t":"C2C_MESSAGE_CREATE","d":{}}`)
	timestamp := "1700000001" // years old → stale
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	sig := hex.EncodeToString(ed25519.Sign(priv, msg.Bytes()))
	req := httptest.NewRequest(http.MethodPost, "/qqbot", bytes.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale push status = %d, want 401", rec.Code)
	}
}

// TestWebhookDedupesRedelivery verifies a redelivered push id runs the handler once.
func TestWebhookDedupesRedelivery(t *testing.T) {
	calls := 0
	s := NewWebhookServer(testSecret, "/qqbot", func(context.Context, *Payload) { calls++ }, nil)
	_, priv := deriveEd25519(testSecret)
	body := []byte(`{"id":"evt-dup","op":0,"t":"C2C_MESSAGE_CREATE","d":{}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	sig := hex.EncodeToString(ed25519.Sign(priv, msg.Bytes()))
	send := func() {
		req := httptest.NewRequest(http.MethodPost, "/qqbot", bytes.NewReader(body))
		req.Header.Set("X-Signature-Ed25519", sig)
		req.Header.Set("X-Signature-Timestamp", timestamp)
		s.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}
	send()
	send()
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (dedup)", calls)
	}
}

// TestWebhookRejectsOracleToken verifies the op-13 plain_token guard refuses a
// JSON-shaped token (which would let the validation path sign a forged push body).
func TestWebhookRejectsOracleToken(t *testing.T) {
	s := NewWebhookServer(testSecret, "/qqbot", nil, nil)
	body, _ := json.Marshal(Payload{
		Op:   OpCallbackValidation,
		Data: mustJSON(CallbackValidation{PlainToken: `{"op":0,"t":"C2C_MESSAGE_CREATE"}`, EventTs: "1725442341"}),
	})
	req := httptest.NewRequest(http.MethodPost, "/qqbot", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oracle-token status = %d, want 400", rec.Code)
	}
}

func TestDeriveEd25519Deterministic(t *testing.T) {
	pub1, _ := deriveEd25519(testSecret)
	pub2, _ := deriveEd25519(testSecret)
	if !bytes.Equal(pub1, pub2) {
		t.Errorf("key derivation is not deterministic")
	}
	if len(pub1) != 32 {
		t.Errorf("public key length = %d, want 32", len(pub1))
	}
	if strings.Contains(string(pub1), "DMI1") {
		t.Errorf("public key should not equal seed")
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
