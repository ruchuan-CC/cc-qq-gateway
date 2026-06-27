package qq

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// deriveEd25519 builds the deterministic Ed25519 keypair from the bot secret,
// per the official webhook spec: the seed is the secret repeated until it is at
// least 32 bytes, then truncated to 32 bytes.
func deriveEd25519(botSecret string) (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := botSecret
	for len(seed) < ed25519.SeedSize {
		seed = strings.Repeat(seed, 2)
	}
	seed = seed[:ed25519.SeedSize]
	pub, priv, _ := ed25519.GenerateKey(strings.NewReader(seed))
	return pub, priv
}

// WebhookServer receives QQ event pushes over HTTP and verifies their Ed25519
// signatures. It answers the op-13 URL validation challenge automatically.
type WebhookServer struct {
	secret  string
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	handler EventHandler
	logger  *log.Logger
	path    string
}

// NewWebhookServer builds a webhook transport. path is the HTTP route to serve
// (e.g. "/qqbot").
func NewWebhookServer(botSecret, path string, handler EventHandler, logger *log.Logger) *WebhookServer {
	if logger == nil {
		logger = log.Default()
	}
	if path == "" {
		path = "/qqbot"
	}
	pub, priv := deriveEd25519(botSecret)
	return &WebhookServer{
		secret:  botSecret,
		pub:     pub,
		priv:    priv,
		handler: handler,
		logger:  logger,
		path:    path,
	}
}

// Path returns the configured HTTP route.
func (s *WebhookServer) Path() string { return s.path }

// Handler returns the http.Handler that processes QQ pushes.
func (s *WebhookServer) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// op 13: URL validation challenge. This arrives before a signature header is
	// established, so sign event_ts+plain_token and reply directly.
	if p.Op == OpCallbackValidation {
		var v CallbackValidation
		if err := json.Unmarshal(p.Data, &v); err != nil {
			http.Error(w, "bad validation payload", http.StatusBadRequest)
			return
		}
		var msg bytes.Buffer
		msg.WriteString(v.EventTs)
		msg.WriteString(v.PlainToken)
		sig := ed25519.Sign(s.priv, msg.Bytes())
		resp := CallbackValidationResponse{
			PlainToken: v.PlainToken,
			Signature:  hex.EncodeToString(sig),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		s.logger.Printf("[webhook] answered URL validation challenge")
		return
	}

	// Verify the signature of normal pushes: verify(timestamp + rawBody).
	if !s.verify(r, body) {
		s.logger.Printf("[webhook] signature verification failed")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Dispatch op-0 events to the handler.
	if p.Op == OpDispatch && s.handler != nil {
		s.handler(r.Context(), &p)
	}

	// Acknowledge receipt.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"op":12,"d":{}}`))
}

func (s *WebhookServer) verify(r *http.Request, body []byte) bool {
	sigHex := r.Header.Get("X-Signature-Ed25519")
	timestamp := r.Header.Get("X-Signature-Timestamp")
	if sigHex == "" || timestamp == "" {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	return ed25519.Verify(s.pub, msg.Bytes(), sig)
}

// Verify exposes signature verification for testing.
func (s *WebhookServer) Verify(timestamp string, body, sig []byte) bool {
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)
	return ed25519.Verify(s.pub, msg.Bytes(), sig)
}

// SignValidation exposes the op-13 signing for testing.
func (s *WebhookServer) SignValidation(eventTs, plainToken string) string {
	var msg bytes.Buffer
	msg.WriteString(eventTs)
	msg.WriteString(plainToken)
	return hex.EncodeToString(ed25519.Sign(s.priv, msg.Bytes()))
}
